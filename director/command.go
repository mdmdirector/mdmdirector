package director

import (
	"bytes"
	"encoding/json"
	intErrors "errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"gorm.io/gorm"
)

func SendCommand(commandPayload types.CommandPayload) (types.Command, error) {
	// Use NanoMDM client if enabled
	if utils.MDMServerType() == string(mdm.ServerTypeNanoMDM) {
		nanoClient, err := mdm.Client()
		if err != nil {
			return types.Command{}, err
		}
		return sendCommandWithClient(nanoClient, commandPayload)
	}

	var command types.Command
	var commandResponse types.CommandResponse
	device, err := GetDevice(commandPayload.UDID)
	if err != nil {
		return command, err
	}

	InfoLogger(
		LogHolder{
			Message:            "Sending Command",
			DeviceUDID:         device.UDID,
			DeviceSerial:       device.SerialNumber,
			CommandRequestType: commandPayload.RequestType,
		},
	)

	// MicroMDM implementation
	jsonStr, err := json.Marshal(commandPayload)
	if err != nil {
		return command, err
	}
	req, _ := http.NewRequest("POST", utils.MicroMDMURL()+"/v1/commands", bytes.NewBuffer(jsonStr))

	req.SetBasicAuth("micromdm", utils.MicroMDMAPIKey())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		if utils.Prometheus() {
			metrics.EnqueueRequests(commandPayload.RequestType, "error").Inc()
		}
		return command, err
	}

	err = json.NewDecoder(resp.Body).Decode(&commandResponse)

	if err != nil {
		if utils.Prometheus() {
			metrics.EnqueueRequests(commandPayload.RequestType, "error").Inc()
		}
		return command, err
	}

	defer resp.Body.Close()
	if utils.Prometheus() {
		metrics.EnqueueRequests(commandPayload.RequestType, metrics.ResultLabel(resp.StatusCode)).Inc()
	}

	command.DeviceUDID = commandPayload.UDID
	command.CommandUUID = commandResponse.Payload.CommandUUID
	command.RequestType = commandPayload.RequestType
	command.Identifier = commandPayload.Identifier
	command.ManifestURL = commandPayload.ManifestURL
	command.ContentHash = commandPayload.ContentHash

	InfoLogger(
		LogHolder{
			Message:            "Sent Command",
			DeviceUDID:         device.UDID,
			DeviceSerial:       device.SerialNumber,
			CommandRequestType: commandPayload.RequestType,
			CommandUUID:        command.CommandUUID,
		},
	)

	db.DB.Create(&command)

	return command, nil
}

// sendCommandWithClient sends a command via NanoMDM using the provided client
func sendCommandWithClient(nanoClient *mdm.NanoMDMClient, commandPayload types.CommandPayload) (types.Command, error) {
	var command types.Command

	device, err := GetDevice(commandPayload.UDID)
	if err != nil {
		return command, err
	}

	InfoLogger(LogHolder{
		Message:            "Sending Command",
		DeviceUDID:         device.UDID,
		DeviceSerial:       device.SerialNumber,
		CommandRequestType: commandPayload.RequestType,
	})
	InfoLogger(LogHolder{DeviceUDID: device.UDID, Message: "Sending command to device via NanoMDM"})

	resp, err := nanoClient.Enqueue([]string{commandPayload.UDID}, commandPayload, nil)
	if err != nil {
		if utils.Prometheus() {
			metrics.EnqueueRequests(commandPayload.RequestType, "error").Inc()
		}
		return command, errors.Wrap(err, "nanoMDM enqueue")
	}

	// Check per-device errors
	pushErr, cmdErr := resp.ErrorsForID(commandPayload.UDID)
	if cmdErr != "" {
		if utils.Prometheus() {
			metrics.EnqueueRequests(commandPayload.RequestType, "error").Inc()
		}
		return command, errors.Errorf("command enqueue failed: %s", cmdErr)
	}
	if utils.Prometheus() {
		metrics.EnqueueRequests(commandPayload.RequestType, "success").Inc()
	}

	if pushErr != "" {
		ErrorLogger(LogHolder{
			Message:      fmt.Sprintf("Push notification failed, command queued: %s", pushErr),
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
		})
	}

	command.DeviceUDID = commandPayload.UDID
	command.CommandUUID = resp.CommandUUID
	command.RequestType = resp.RequestType
	command.Identifier = commandPayload.Identifier
	command.ManifestURL = commandPayload.ManifestURL
	command.ContentHash = commandPayload.ContentHash

	InfoLogger(LogHolder{
		Message:            "Sent Command",
		DeviceUDID:         device.UDID,
		DeviceSerial:       device.SerialNumber,
		CommandRequestType: commandPayload.RequestType,
		CommandUUID:        command.CommandUUID,
	})

	db.DB.Create(&command)

	return command, nil
}

func UpdateCommand(
	ackEvent *types.AcknowledgeEvent,
	device types.Device,
	payloadDict map[string]interface{},
) error {
	var command types.Command

	if device.UDID == "" {
		log.Errorf("Cannot update command %v without a device UDID!!!!", ackEvent.CommandUUID)
	}

	commandRequestType := "unknown"

OuterLoop:
	for k := range payloadDict {
		switch k {
		case "ProfileList":
			commandRequestType = k
			break OuterLoop
		case "SecurityInfo":
			commandRequestType = k
			break OuterLoop
		case "CertificateList":
			commandRequestType = k
			break OuterLoop
		case "QueryResponses":
			commandRequestType = k
			break OuterLoop
		case "DeviceInformation":
			commandRequestType = k
			break OuterLoop
		}
	}

	if commandRequestType == "unknown" {
		err := db.DB.Model(&command).
			Select("request_type").
			Where("command_uuid = ?", ackEvent.CommandUUID).
			First(&command).
			Error
		if err != nil {
			if intErrors.Is(err, gorm.ErrRecordNotFound) {
				InfoLogger(LogHolder{Message: "Command not found in queue"})
			}
		} else {
			commandRequestType = command.RequestType
		}
	}

	InfoLogger(
		LogHolder{
			Message:            "Command response received",
			CommandStatus:      ackEvent.Status,
			CommandUUID:        ackEvent.CommandUUID,
			DeviceUDID:         device.UDID,
			DeviceSerial:       device.SerialNumber,
			CommandRequestType: commandRequestType,
		},
	)

	if err := db.DB.Where("device_ud_id = ? AND command_uuid = ?", device.UDID, ackEvent.CommandUUID).Error; err != nil {
		if intErrors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("Command not found in the queue")
		}
	} else {
		if ackEvent.Status == "Error" {
			InfoLogger(LogHolder{Message: "Error response received", Metric: string(ackEvent.RawPayload), DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber})
			err := db.DB.Model(&command).Select("status", "error_string").Where("device_ud_id = ? AND command_uuid = ?", device.UDID, ackEvent.CommandUUID).Updates(types.Command{
				Status:      ackEvent.Status,
				ErrorString: string(ackEvent.RawPayload),
			}).Error
			if err != nil {
				return err
			}
		} else {
			err := db.DB.Model(&command).Select("status", "error_string").Where("device_ud_id = ? AND command_uuid = ?", device.UDID, ackEvent.CommandUUID).Updates(types.Command{
				Status:      ackEvent.Status,
				ErrorString: "",
			}).Error
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// CommandInQueue reports whether a command of requestType is already pending for the
// device. For profile commands ("InstallProfile"/"RemoveProfile"), pass the profile's
// identifier so distinct profiles aren't deduped against each other; for commands that
// aren't profile-scoped (e.g. "SecurityInfo", "DeviceInformation"), pass "".
func CommandInQueue(device types.Device, requestType string, identifier string) (bool, error) {
	var commandModel types.Command

	err := db.DB.Model(&commandModel).
		Where("device_ud_id = ? AND request_type = ? AND identifier = ?", device.UDID, requestType, identifier).
		Where("status = ? OR status = ?", "", "NotNow").
		First(&commandModel).
		Error
	if err != nil {
		if intErrors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, errors.Wrap(err, "command in queue")
	}

	return true, nil
}

// ResolveProfileCommandInQueue checks whether an InstallProfile command for this device
// and profile identifier is already pending delivery. If the pending command's content
// no longer matches contentHash (the profile's current HashedPayloadUUID), it rewrites
// the pending command in place with the fresh payload/hash rather than leaving it stale
// or enqueuing a duplicate: this closes the window where a profile's content changes
// while an earlier InstallProfile command for the same identifier is still queued, so the
// device gets the latest content on its next checkin instead of the outdated one.
func ResolveProfileCommandInQueue(device types.Device, identifier string, contentHash string, payload string) (bool, error) {
	var commandModel types.Command

	err := db.DB.Model(&commandModel).
		Where("device_ud_id = ? AND request_type = ? AND identifier = ?", device.UDID, "InstallProfile", identifier).
		Where("status = ? OR status = ?", "", "NotNow").
		First(&commandModel).
		Error
	if err != nil {
		if intErrors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, errors.Wrap(err, "resolve profile command in queue")
	}

	if commandModel.ContentHash == contentHash {
		return true, nil
	}

	if err := db.DB.Model(&types.Command{}).
		Where("command_uuid = ?", commandModel.CommandUUID).
		Updates(map[string]interface{}{
			"payload":      payload,
			"content_hash": contentHash,
		}).Error; err != nil {
		return false, errors.Wrap(err, "refresh stale queued profile command")
	}

	return true, nil
}

func InstallAppInQueue(device types.Device, manifestURL string) (bool, error) {
	var commandModel types.Command

	err := db.DB.Model(&commandModel).
		Where("device_ud_id = ? AND request_type = ? AND manifest_url = ?", device.UDID, "InstallApplication", manifestURL).
		Where("status = ? OR status = ?", "", "NotNow").
		First(&commandModel).
		Error
	if err != nil {
		if intErrors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, errors.Wrap(err, "Install App in queue")
	}

	return true, nil
}

func ClearCommands(device *types.Device) error {
	var command types.Command
	var commands []types.Command
	InfoLogger(
		LogHolder{
			Message:      "Clearing command queue",
			DeviceSerial: device.SerialNumber,
			DeviceUDID:   device.UDID,
		},
	)
	err := db.DB.Model(&command).
		Where("device_ud_id = ?", device.UDID).
		Not("status = ? OR status = ?", "Error", "Acknowledged").
		Delete(&commands).
		Error
	if err != nil {
		return errors.Wrapf(err, "Failed to clear Command Queue for %v", device.UDID)
	}

	clearDevice := utils.FlagProvider.ClearDeviceOnEnroll()
	if clearDevice {
		var deviceProfile types.DeviceProfile
		var deviceProfiles []types.DeviceProfile
		err := db.DB.Model(&deviceProfile).
			Where("device_ud_id = ?", device.UDID).
			Delete(&deviceProfiles).
			Error
		if err != nil {
			return errors.Wrapf(err, "Failed to clear Device Profiles for %v", device.UDID)
		}

		var deviceInstallApplication types.DeviceInstallApplication
		var deviceInstallApplications []types.DeviceInstallApplication
		err = db.DB.Model(&deviceInstallApplication).
			Where("device_ud_id = ?", device.UDID).
			Delete(&deviceInstallApplications).
			Error
		if err != nil {
			return errors.Wrapf(
				err,
				"Failed to clear Device InstalApplications for %v",
				device.UDID,
			)
		}

	}

	return nil
}

func GetAllCommands(w http.ResponseWriter, r *http.Request) {
	var commands []types.Command

	err := db.DB.Find(&commands).Scan(&commands).Error
	if err != nil {
		log.Errorf("Couldn't scan to Commands model: %v", err)
	}
	output, err := json.MarshalIndent(&commands, "", "    ")
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
}

// staleCommandExemptRequestTypes are never expired by expireStaleCommands,
// regardless of how long they've sat unresolved. DeviceLock/EraseDevice are
// one-shot, high-consequence commands - CommandInQueue treating one as
// expired would let a retry path enqueue a second lock/wipe for the same
// device while the original might still be in flight, which could wipe a
// device twice or stack conflicting lock PINs. Better to leave the row
// blocking retries than risk a duplicate.
var staleCommandExemptRequestTypes = []string{"DeviceLock", "EraseDevice"}

// expireStaleCommands deletes local Command bookkeeping rows that have sat
// with an empty (never-acknowledged) status for longer than
// utils.StaleCommandThreshold() minutes (defaults to 5 days; override with
// --stale-command-threshold or STALE_COMMAND_THRESHOLD), excluding
// staleCommandExemptRequestTypes. NanoMDM's own Authenticate check-in handler
// unconditionally clears any unresolved queue entries for a device, per the
// MDM spec, to avoid stale commands surviving an unenrollment. If a device
// re-authenticates while RunInitialTasks/InstallAllProfiles is still
// mid-flight sending its serial batch of commands, NanoMDM can silently
// deactivate the not-yet-acknowledged ones before the device ever requests
// them. mdmdirector has no visibility into that: the local Command row is
// left at status="" forever, and CommandInQueue then treats it as "already
// queued", permanently blocking any future retry of that device+profile.
// Expiring the row here unblocks CommandInQueue so the next scheduled push or
// ProfileList verification can retry instead of treating the device as
// permanently caught up.
func expireStaleCommands() error {
	var commands []types.Command
	threshold := time.Duration(utils.StaleCommandThreshold()) * time.Minute
	cutoff := time.Now().Add(-threshold)
	err := db.DB.Where("status = ? AND updated_at < ? AND request_type NOT IN ?", "", cutoff, staleCommandExemptRequestTypes).Find(&commands).Error
	if err != nil {
		return errors.Wrap(err, "expireStaleCommands: find")
	}
	if len(commands) == 0 {
		return nil
	}
	if err := db.DB.Delete(&commands).Error; err != nil {
		return errors.Wrap(err, "expireStaleCommands: delete")
	}
	InfoLogger(LogHolder{Message: fmt.Sprintf("Expired %d stale command(s) with no response after %s", len(commands), threshold)})
	return nil
}

func GetPendingCommands(w http.ResponseWriter, r *http.Request) {
	var commands []types.Command

	err := db.DB.Find(&commands).
		Where("status = ? OR status = ?", "", "NotNow").
		Scan(&commands).
		Error
	if err != nil {
		log.Errorf("Couldn't scan to Commands model: %v", err)
	}
	output, err := json.MarshalIndent(&commands, "", "    ")
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
}

func DeletePendingCommands(w http.ResponseWriter, r *http.Request) {
	var commands []types.Command

	err := db.DB.Find(&commands).
		Where("status = ? OR status = ?", "", "NotNow").
		Scan(&commands).
		Delete(&commands).
		Error
	if err != nil {
		log.Errorf("Couldn't scan to Commands model: %v", err)
	}
	// output, err := json.MarshalIndent(&commands, "", "    ")
	// if err != nil {
	// 	ErrorLogger(LogHolder{Message: err.Error()})
	// 	w.WriteHeader(http.StatusInternalServerError)
	// }

	// w.Write(output)
}

func GetErrorCommands(w http.ResponseWriter, r *http.Request) {
	var commands []types.Command

	err := db.DB.Find(&commands).Where("status = ?", "Error").Scan(&commands).Error
	if err != nil {
		log.Errorf("Couldn't scan to Commands model: %v", err)
	}
	output, err := json.MarshalIndent(&commands, "", "    ")
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
}

func ExpireCommands() error {
	var commands []types.Command
	thirtyDaysAgo := time.Now().Add(-720 * time.Hour)
	err := db.DB.Unscoped().
		Find(&commands).
		Where("status = ? OR status = ?", "", "NotNow").
		Where("updated_at < ?", thirtyDaysAgo).
		Delete(&commands).
		Error
	if err != nil {
		return err
	}

	return nil
}

func clearCommandQueue(device types.Device) error {
	// Use NanoMDM client if enabled
	if utils.MDMServerType() == string(mdm.ServerTypeNanoMDM) {
		nanoClient, err := mdm.Client()
		if err != nil {
			return err
		}
		return clearCommandQueueWithClient(nanoClient, device)
	}

	// MicroMDM implementation
	var httpClient = &http.Client{
		Timeout: time.Second * 1,
	}

	endpoint, err := url.Parse(utils.MicroMDMURL())
	if err != nil {
		return err
	}

	endpoint.Path = path.Join(endpoint.Path, "v1", "commands", device.UDID)

	req, _ := http.NewRequest("DELETE", endpoint.String(), bytes.NewBufferString("{}"))
	req.SetBasicAuth("micromdm", utils.MicroMDMAPIKey())
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}

	return resp.Body.Close()
}

func InspectCommandQueue(device types.Device) ([]byte, error) {
	// Use NanoMDM client if enabled
	if utils.MDMServerType() == string(mdm.ServerTypeNanoMDM) {
		nanoClient, err := mdm.Client()
		if err != nil {
			return nil, err
		}
		return inspectCommandQueueWithClient(nanoClient, device)
	}

	// MicroMDM implementation
	endpoint, err := url.Parse(utils.MicroMDMURL())
	if err != nil {
		return nil, err
	}

	endpoint.Path = path.Join(endpoint.Path, "v1", "commands", device.UDID)
	req, _ := http.NewRequest("GET", endpoint.String(), nil)
	req.SetBasicAuth("micromdm", utils.MicroMDMAPIKey())

	httpClient := &http.Client{
		Timeout: time.Second * 10,
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, errors.Wrap(err, "failed to read response body")
	}
	return buf.Bytes(), nil
}

// clearCommandQueueWithClient clears the NanoMDM command queue using the provided client
func clearCommandQueueWithClient(nanoClient *mdm.NanoMDMClient, device types.Device) error {
	_, err := nanoClient.ClearQueue(device.UDID)
	if err != nil {
		return errors.Wrap(err, "clearCommandQueue via NanoMDM")
	}
	return nil
}

// inspectCommandQueueWithClient inspects the NanoMDM command queue using the provided client
func inspectCommandQueueWithClient(nanoClient *mdm.NanoMDMClient, device types.Device) ([]byte, error) {
	resp, err := nanoClient.InspectQueue(device.UDID)
	if err != nil {
		return nil, errors.Wrap(err, "InspectCommandQueue via NanoMDM")
	}

	// Convert nanoMDM response to microMDM-compatible format
	unified, err := mdm.ConvertToUnifiedResponse(resp)
	if err != nil {
		return nil, errors.Wrap(err, "InspectCommandQueue: convert response")
	}

	jsonData, err := json.Marshal(unified)
	if err != nil {
		return nil, errors.Wrap(err, "InspectCommandQueue: marshal response")
	}
	return jsonData, nil
}
