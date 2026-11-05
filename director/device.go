package director

import (
	"encoding/json"
	intErrors "errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"gorm.io/gorm"
)

func UpdateDevice(newDevice types.Device) (*types.Device, error) {
	var device types.Device

	if newDevice.UDID == "" && newDevice.SerialNumber == "" {
		err := fmt.Errorf("no device UDID or serial set")
		return &newDevice, errors.Wrap(err, "UpdateDevice")
	}
	now := time.Now()

	newDevice.LastCheckedIn = now
	if newDevice.UDID != "" {
		if err := db.DB.Where("ud_id = ?", newDevice.UDID).First(&device).Error; err != nil {
			if intErrors.Is(err, gorm.ErrRecordNotFound) {
				if err := db.DB.Create(&newDevice).Error; err != nil {
					return &newDevice, errors.Wrap(err, "Update device create udid")
				}
				device = newDevice
			}
		} else {
			err := db.DB.Model(&device).Where("ud_id = ?", newDevice.UDID).Assign(&newDevice).FirstOrCreate(&device).Error
			if err != nil {
				return &newDevice, errors.Wrap(err, "Update device first or create udid")
			}
		}
	}

	if newDevice.SerialNumber != "" {
		if err := db.DB.Where("serial_number = ?", newDevice.SerialNumber).First(&device).Error; err != nil {
			if intErrors.Is(err, gorm.ErrRecordNotFound) {
				if err := db.DB.Create(&newDevice).Error; err != nil {
					return &newDevice, errors.Wrap(err, "Update device create serial")
				}
				device = newDevice
			}
		} else {
			err := db.DB.Model(&device).Where("serial_number = ?", newDevice.SerialNumber).Assign(&newDevice).FirstOrCreate(&device).Error
			if err != nil {
				return &newDevice, errors.Wrap(err, "Update device first or create serial")
			}
		}
	}

	return &device, nil
}

// UpdateDeviceBools persists the boolean fields that are only ever populated
// by an actual DeviceInformation query response (IsSupervised,
// IsActivationLockEnabled, etc.) via an explicit map-based Updates call,
// bypassing GORM's default skip-zero-value behavior for struct updates so a
// legitimate true -> false transition can still persist.
//
// Callers MUST only invoke this with a Device decoded from a DeviceInformation
// QueryResponses payload. Checkin and other Acknowledge payloads (ProfileList,
// SecurityInfo, CertificateList, command acks, etc.) don't carry these fields
// at all, so a Device decoded from one of those has them at their Go zero
// value (false) rather than any real value from the device - calling this with
// such a Device would silently clobber a correct value back to false.
func UpdateDeviceBools(newDevice *types.Device) error {
	var deviceModel types.Device
	err := db.DB.Model(&deviceModel).
		Select("is_supervised", "is_device_locator_service_enabled", "is_activation_lock_enabled", "is_do_not_disturb_in_effect", "is_cloud_backup_enabled", "system_integrity_protection_enabled", "app_analytics_enabled", "is_mdm_lost_mode_enabled", "awaiting_configuration", "diagnostic_submission_enabled", "is_multi_user").
		Where("ud_id = ?", newDevice.UDID).
		Updates(map[string]interface{}{
			"is_supervised":                       newDevice.IsSupervised,
			"is_device_locator_service_enabled":   newDevice.IsDeviceLocatorServiceEnabled,
			"is_activation_lock_enabled":          newDevice.IsActivationLockEnabled,
			"is_do_not_disturb_in_effect":         newDevice.IsDoNotDisturbInEffect,
			"is_cloud_backup_enabled":             newDevice.IsCloudBackupEnabled,
			"system_integrity_protection_enabled": newDevice.SystemIntegrityProtectionEnabled,
			"app_analytics_enabled":               newDevice.AppAnalyticsEnabled,
			"is_mdm_lost_mode_enabled":            newDevice.IsMDMLostModeEnabled,
			"awaiting_configuration":              newDevice.AwaitingConfiguration,
			"diagnostic_submission_enabled":       newDevice.DiagnosticSubmissionEnabled,
			"is_multi_user":                       newDevice.IsMultiUser,
		}).
		Error
	if err != nil {
		return err
	}

	return nil
}

func GetDevice(udid string) (types.Device, error) {
	var device types.Device

	if udid == "" {
		err := fmt.Errorf("no device UDID set")
		return device, errors.Wrap(err, "GetDevice")
	}

	err := db.DB.Where("ud_id = ?", udid).First(&device).Error
	if err != nil {
		return device, errors.Wrapf(
			err,
			"Couldn't scan to Device model from GetDevice %v",
			device.UDID,
		)
	}
	return device, nil
}

func GetDeviceSerial(serial string) (types.Device, error) {
	var device types.Device

	if serial == "" {
		err := fmt.Errorf("no device Serial passed")
		return device, errors.Wrap(err, "GetDeviceSerial")
	}

	err := db.DB.Model(device).
		Where("serial_number = ?", serial).
		Order("last_checked_in desc").
		First(&device).
		Scan(&device).
		Error
	if err != nil {
		return device, errors.Wrap(err, "GetDeviceSerial")
	}
	return device, nil
}

func GetAllDevices() ([]types.Device, error) {
	var devices []types.Device

	err := db.DB.Preload("OSUpdateSettings").
		Preload("SecurityInfo").
		Preload("SecurityInfo.FirmwarePasswordStatus").
		Preload("SecurityInfo.ManagementStatus").
		Preload("SecurityInfo.FirewallSettings").
		Preload("SecurityInfo.SecureBoot").
		Preload("SecurityInfo.SecureBoot.SecureBootReducedSecurity").
		Find(&devices).
		Error
	if err != nil {
		return devices, errors.Wrap(err, "Get All Devices")
	}
	return devices, nil
}

func GetAllDevicesAndAssociations() ([]types.Device, error) {
	var devices []types.Device

	err := db.DB.Preload("OSUpdateSettings").
		Preload("SecurityInfo").
		Preload("SecurityInfo.FirmwarePasswordStatus").
		Preload("SecurityInfo.ManagementStatus").
		Preload("SecurityInfo.FirewallSettings").
		Preload("SecurityInfo.SecureBoot").
		Preload("SecurityInfo.SecureBoot.SecureBootReducedSecurity").
		Preload("Certificates").
		Preload("ProfileList").
		Find(&devices).
		Error
	if err != nil {
		return devices, errors.Wrap(
			err,
			"Couldn't scan to Device model from GetAllDevicesAndAssociations",
		)
	}

	return devices, nil
}

// PostDeviceCommandHandler applies command to every device in the payload. A device that
// can't be found, or whose command fails, is logged and skipped so the rest are still
// processed; the response is then a single 500 naming the devices that failed.
func PostDeviceCommandHandler(w http.ResponseWriter, r *http.Request) {
	var out types.DeviceCommandPayload
	var devices []types.Device
	var failed []string
	vars := mux.Vars(r)

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
		return
	}

	command := vars["command"]
	pushNow := out.PushNow
	value := out.Value
	pin := out.Pin
	if out.DeviceUDIDs != nil {
		for i := range out.DeviceUDIDs {
			device, err := GetDevice(out.DeviceUDIDs[i])
			if err != nil {
				ErrorLogger(LogHolder{DeviceUDID: out.DeviceUDIDs[i], Message: err.Error()})
				failed = append(failed, out.DeviceUDIDs[i])
				continue
			}
			devices = append(devices, device)
		}
	}

	if out.SerialNumbers != nil {
		for i := range out.SerialNumbers {
			device, err := GetDeviceSerial(out.SerialNumbers[i])
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: out.SerialNumbers[i], Message: err.Error()})
				failed = append(failed, out.SerialNumbers[i])
				continue
			}
			devices = append(devices, device)
		}
	}

	for i := range devices {
		device := devices[i]
		var deviceModel types.Device
		if command == "clear_queue" {
			err = clearCommandQueue(device)
			if err != nil {
				ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
				failed = append(failed, device.UDID)
			}
			continue
		}
		if command == "device_lock" {
			if pin != "" {
				err := db.DB.Model(&deviceModel).
					Select("lock", "unlock_pin").
					Where("ud_id = ?", device.UDID).
					Updates(map[string]interface{}{
						"lock":       value,
						"unlock_pin": pin,
					}).
					Error
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}
			} else {
				err := db.DB.Model(&deviceModel).Select("lock", "unlock_pin").Where("ud_id = ?", device.UDID).Updates(map[string]interface{}{
					"lock":       value,
					"unlock_pin": "",
				}).Error
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}
			}

		}

		if command == "erase_device" {
			if pin != "" {
				err := db.DB.Model(&deviceModel).
					Select("erase", "unlock_pin").
					Where("ud_id = ?", device.UDID).
					Updates(map[string]interface{}{
						"erase":      value,
						"unlock_pin": pin,
					}).
					Error
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}
			} else {
				err := db.DB.Model(&deviceModel).Select("erase", "unlock_pin").Where("ud_id = ?", device.UDID).Updates(map[string]interface{}{
					"erase":      value,
					"unlock_pin": "",
				}).Error
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}
			}
		}

		if pushNow {
			err = EraseLockDevice(device.UDID)
			if err != nil {
				ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
			}
		}

	}

	if len(failed) > 0 {
		http.Error(
			w,
			fmt.Sprintf("%s failed for: %s", command, strings.Join(failed, ", ")),
			http.StatusInternalServerError,
		)
	}
}

func DeviceHandler(w http.ResponseWriter, r *http.Request) {
	var devices []types.Device
	var err error
	info := r.URL.Query().Get("info")
	if info == "limited" {
		devices, err = GetAllDevices()
	} else {
		devices, err = GetAllDevicesAndAssociations()
	}

	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}

	output, err := json.MarshalIndent(&devices, "", "    ")
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
}

func SingleDeviceOutput(device types.Device, w http.ResponseWriter, r *http.Request) error {
	var err error
	info := r.URL.Query().Get("info")
	if info != "limited" {
		device, err = FetchDeviceAndRelations(device)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
		}
	}

	output, err := json.MarshalIndent(&device, "", "    ")
	if err != nil {
		return err
	}

	_, err = w.Write(output)
	if err != nil {
		return err
	}

	return nil
}

func SingleDeviceHandler(w http.ResponseWriter, r *http.Request) {
	var device types.Device
	vars := mux.Vars(r)

	var err error

	device, err = GetDevice(vars["udid"])
	if err != nil {
		// An unknown UDID is a client error, not a director failure: 404 and
		// log at info rather than error.
		if intErrors.Is(err, gorm.ErrRecordNotFound) {
			InfoLogger(LogHolder{DeviceUDID: vars["udid"], Message: "SingleDeviceHandler: device not found"})
			w.WriteHeader(http.StatusNotFound)
			return
		}
		ErrorLogger(LogHolder{DeviceUDID: vars["udid"], Message: "SingleDeviceHandler: " + err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err = SingleDeviceOutput(device, w, r)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func SingleDeviceSerialHandler(w http.ResponseWriter, r *http.Request) {
	var device types.Device
	vars := mux.Vars(r)

	var err error

	device, err = GetDeviceSerial(vars["serial"])
	if err != nil {
		if strings.Contains(err.Error(), "record not found") {
			ErrorLogger(LogHolder{DeviceSerial: vars["serial"], Message: err.Error()})
			w.WriteHeader(http.StatusNotFound)
		} else {
			ErrorLogger(LogHolder{DeviceSerial: vars["serial"], Message: err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	err = SingleDeviceOutput(device, w, r)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func FetchDeviceModelAndRelations(device types.Device) (types.Device, error) {
	err := db.DB.Preload("OSUpdateSettings").
		Preload("SecurityInfo").
		Preload("SecurityInfo.FirmwarePasswordStatus").
		Preload("SecurityInfo.ManagementStatus").
		Preload("Certificates").
		Preload("ProfileList").
		Preload("Profiles").
		First(&device).
		Error
	if err != nil {
		log.Error("Couldn't scan to Device model from FetchDeviceModelAndRelations", err)
	}

	return device, nil
}

func FetchDeviceAndRelations(device types.Device) (types.Device, error) {
	var empty types.Device
	err := db.DB.Preload("OSUpdateSettings").
		Preload("SecurityInfo").
		Preload("SecurityInfo.FirmwarePasswordStatus").
		Preload("SecurityInfo.ManagementStatus").
		Preload("SecurityInfo.FirewallSettings").
		Preload("SecurityInfo.SecureBoot").
		Preload("SecurityInfo.SecureBoot.SecureBootReducedSecurity").
		Preload("Certificates").
		Preload("ProfileList").
		Preload("Profiles").
		First(&device).
		Error
	if err != nil {
		log.Error("Couldn't scan to Device model from FetchDeviceAndRelations", err)
	}
	if err != nil {
		return empty, errors.Wrap(err, "FetchDeviceAndRelations")
	}

	return device, nil
}

// requestInfoCommand enqueues one of the read-only inventory commands, skipping it when
// an unacknowledged copy is already queued for the device.
//
// opts is passed through to the enqueue. RequestAllDeviceInfo sets NoPush so a batch
// costs one push instead of one per command; every other caller passes nil and keeps the
// push attached to its own enqueue.
func requestInfoCommand(device types.Device, requestType string, queries []string, opts *mdm.EnqueueOptions) error {
	inQueue, err := CommandInQueue(device, requestType, "")
	if err != nil {
		return errors.Wrapf(err, "request %s: CommandInQueue", requestType)
	}
	if inQueue {
		InfoLogger(LogHolder{
			Message:            "Already in queue, not requesting",
			DeviceUDID:         device.UDID,
			DeviceSerial:       device.SerialNumber,
			CommandRequestType: requestType,
		})
		return nil
	}

	InfoLogger(LogHolder{
		Message:            "Requesting " + requestType,
		DeviceUDID:         device.UDID,
		DeviceSerial:       device.SerialNumber,
		CommandRequestType: requestType,
	})

	var payload types.CommandPayload
	payload.UDID = device.UDID
	payload.SerialNumber = device.SerialNumber
	payload.RequestType = requestType
	payload.Queries = queries

	if _, err := SendCommandWithOptions(payload, opts); err != nil {
		return errors.Wrapf(err, "request %s: SendCommand", requestType)
	}

	return nil
}

func RequestDeviceInformation(device types.Device) error {
	return requestInfoCommand(device, "DeviceInformation", types.DeviceInformationQueries, nil)
}

func SetTokenUpdate(device types.Device) (types.Device, error) {
	var deviceModel types.Device
	DebugLogger(
		LogHolder{
			Message:      "TokenUpdate Received",
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
		},
	)
	err := db.DB.Model(&deviceModel).
		Select("token_update_recieved", "authenticate_recieved").
		Where("ud_id = ?", device.UDID).
		Updates(map[string]interface{}{"token_update_recieved": true, "authenticate_recieved": true}).
		Error
	if err != nil {
		return device, errors.Wrap(err, "Set TokenUpdate")
	}
	updatedDevice, err := GetDevice(device.UDID)
	if err != nil {
		return device, errors.Wrap(err, "Set TokenUpdate")
	}
	return updatedDevice, nil
}

// infoBatchRequestTypes are the four inventory commands an idle check-in asks for, in
// the order they are enqueued.
//
//nolint:gochecknoglobals
var infoBatchRequestTypes = []string{
	"ProfileList",
	"SecurityInfo",
	"DeviceInformation",
	"CertificateList",
}

// RequestAllDeviceInfo enqueues the four inventory commands and then sends a single
// push.
//
// Each enqueue used to carry its own push, so one batch woke the device up to four
// times and each of those wakes came back as another Idle check-in. The commands all
// sit in the same NanoMDM queue and the device drains the queue on one wake, so the
// extra pushes bought nothing. The enqueues now set NoPush and one push at the end
// collects all four.
//
// The push is attempted whenever at least one command was enqueued, including when a
// later enqueue failed, so a partial batch is still delivered rather than sitting in the
// queue until something else pushes. A failed enqueue or a failed push returns an error
// and leaves last_info_requested unstamped, so the claim lapses after its TTL and a
// later check-in retries. That retry is cheap: the commands still queued are seen by
// CommandInQueue and skipped, leaving just the push.
//
// MicroMDM has no equivalent of NoPush, so against MicroMDM every enqueue still pushes
// and the trailing push is skipped.
func RequestAllDeviceInfo(device types.Device) error {
	noPush := utils.MDMServerType() == string(mdm.ServerTypeNanoMDM)

	var opts *mdm.EnqueueOptions
	if noPush {
		opts = &mdm.EnqueueOptions{NoPush: true}
	}

	enqueued := 0
	var firstErr error
	for _, requestType := range infoBatchRequestTypes {
		var queries []string
		if requestType == "DeviceInformation" {
			queries = types.DeviceInformationQueries
		}
		if err := requestInfoCommand(device, requestType, queries, opts); err != nil {
			firstErr = errors.Wrap(err, "RequestAllDeviceInfo")
			break
		}
		enqueued++
	}

	if noPush && enqueued > 0 {
		if err := PushDevice(device.UDID); err != nil {
			if firstErr != nil {
				ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: firstErr.Error()})
			}
			return errors.Wrap(err, "RequestAllDeviceInfo:PushDevice")
		}
	}

	if firstErr != nil {
		return firstErr
	}

	now := time.Now()
	var deviceModel types.Device
	err := db.DB.Model(&deviceModel).
		Select("last_info_requested").
		Where("ud_id = ?", device.UDID).
		Updates(map[string]interface{}{
			"last_info_requested": now,
		}).
		Error
	if err != nil {
		return err
	}

	return nil
}

func PushDeviceHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	udid := vars["udid"]
	InfoLogger(LogHolder{DeviceUDID: udid, Message: "Request to /device/push received"})
	err := PushDevice(udid)
	if err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		_, err = w.Write([]byte("{\"status\":\"push_sent\"}"))
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
	}
}

func InspectDeviceCommands(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	udid := vars["udid"]
	InfoLogger(LogHolder{DeviceUDID: udid, Message: fmt.Sprintf("Request to /device/%s/commands received", udid)})

	device, err := GetDevice(udid)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	response, err := InspectCommandQueue(device)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(response)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
}
