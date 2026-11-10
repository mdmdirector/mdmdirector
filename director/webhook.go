package director

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/groob/plist"
	"github.com/hashicorp/go-version"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func profileListDataJSON(profileListData types.ProfileListData) ([]byte, error) {
	var metricMap []map[string]string
	for i := range profileListData.ProfileList {
		metricMap = append(metricMap, make(map[string]string))
		payload := profileListData.ProfileList[i]
		metricMap[i]["PayloadIdentifier"] = payload.PayloadIdentifier
		metricMap[i]["PayloadUUID"] = payload.PayloadUUID
	}
	jsonBlob, err := func(t []map[string]string) ([]byte, error) {
		buffer := &bytes.Buffer{}
		encoder := json.NewEncoder(buffer)
		encoder.SetEscapeHTML(false)
		err := encoder.Encode(t)
		return buffer.Bytes(), err
	}(metricMap)
	if err != nil {
		return nil, errors.Wrapf(err, "problem with creating jsonblob")
	}

	return jsonBlob, nil
}

func WebhookHandler(w http.ResponseWriter, r *http.Request) {
	var out types.PostPayload
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		return
	}

	if out.CheckinEvent != nil {
		err := handleCheckinEvent(out.Topic, out.CheckinEvent)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
		if utils.Prometheus() {
			metrics.CheckinRequests(checkinMessageType(out.Topic), metrics.ResultFromError(err)).Inc()
		}
		return
	}

	if out.AcknowledgeEvent != nil {
		err := handleAcknowledgeEvent(out.AcknowledgeEvent)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
		if utils.Prometheus() {
			metrics.CommandResults(commandResultStatus(out.AcknowledgeEvent.Status), metrics.ResultFromError(err)).Inc()
		}
	}
}

// commandResultStatus normalizes the device-reported Status field into a label for mdmdirector_command_results_total
func commandResultStatus(status string) string {
	switch status {
	case "Acknowledged", "Error", "Idle", "NotNow", "CommandFormatError":
		return status
	default:
		return metrics.UnknownLabel
	}
}

// checkinMessageType maps an MDM webhook topic (e.g. "mdm.TokenUpdate") to the MessageType label used by mdmdirector_checkin_requests_total
func checkinMessageType(topic string) string {
	const prefix = "mdm."
	if !strings.HasPrefix(topic, prefix) {
		return metrics.UnknownLabel
	}
	switch t := strings.TrimPrefix(topic, prefix); t {
	case "Authenticate", "TokenUpdate", "CheckOut":
		return t
	default:
		return metrics.UnknownLabel
	}
}

// reconcileDeviceState handles post-enrollment lifecycle transitions after any device event
func reconcileDeviceState(currentDevice *types.Device) error {
	if !currentDevice.InitialTasksRun && currentDevice.TokenUpdateRecieved {
		// RunInitialTasks sends dozens of commands, and every acknowledgement comes back
		// through this webhook while initial_tasks_run is still false. Without this check
		// each one re-enters RunInitialTasks, loses the lease, and counts as
		// lease_contention - ~50 spurious skips per enrollment across 6 replicas.
		if initialTasksInFlight(currentDevice) {
			DebugLogger(LogHolder{DeviceSerial: currentDevice.SerialNumber, DeviceUDID: currentDevice.UDID, Message: "Initial tasks already in flight; not re-entering"})
			return nil
		}
		InfoLogger(LogHolder{DeviceSerial: currentDevice.SerialNumber, DeviceUDID: currentDevice.UDID, Message: "Running initial tasks"})
		if err := RunInitialTasks(currentDevice.UDID); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: currentDevice.UDID, DeviceSerial: currentDevice.SerialNumber, Message: err.Error()})
			return err
		}
		return nil
	}

	// awaiting_configuration is set from a DeviceInformation response. Initial tasks have
	// already sent DeviceConfigured and cleared it, so the flag being back on means the
	// device reported it is still in Setup Assistant: send again, and clear the flag
	// straight away so the acks for this send don't come back through here and send more.
	if currentDevice.AwaitingConfiguration && currentDevice.InitialTasksRun {
		InfoLogger(LogHolder{DeviceSerial: currentDevice.SerialNumber, DeviceUDID: currentDevice.UDID, Message: "Device reports it is still awaiting configuration; resending DeviceConfigured"})
		if err := SendDeviceConfigured(*currentDevice); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: currentDevice.UDID, DeviceSerial: currentDevice.SerialNumber, Message: err.Error()})
			return err
		}
		if err := clearAwaitingConfiguration(currentDevice); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: currentDevice.UDID, DeviceSerial: currentDevice.SerialNumber, Message: err.Error()})
			return err
		}
	}

	return nil
}

// userChannelIDs holds the keys that only a user-channel MDM message carries.
type userChannelIDs struct {
	UserID           string `plist:",omitempty"`
	EnrollmentUserID string `plist:",omitempty"`
}

// isUserChannel reports whether a raw check-in came from a user channel. A user-channel
// check-in carries the device's UDID too, so without this check its Authenticate,
// TokenUpdate and CheckOut drive the device channel's enrollment state. nanomdm still
// stores the user channel, so commands can be sent to it regardless.
//
// That matters at enrollment: macOS sends the device and user TokenUpdates within
// milliseconds of each other. When the user one is processed first, the device is
// marked token_update_recieved and RunInitialTasks starts queueing commands while
// nanomdm still stores the previous enrollment's PushMagic for the device channel.
// Every push sent in that window is dropped by the device as a PushMagic mismatch.
func isUserChannel(rawPayload []byte) bool {
	var ids userChannelIDs
	if err := plist.Unmarshal(rawPayload, &ids); err != nil {
		return false
	}
	return ids.UserID != "" || ids.EnrollmentUserID != ""
}

// deviceUDIDFromEnrollmentID returns the device UDID segment of an MDM enrollment ID.
//
// NanoMDM's user-channel enrollment ID is "<deviceUDID>:<userID>", and those
// messages legitimately carry the bare device UDID in their raw payload. Comparing
// the whole string would therefore reject every user-channel check-in fleet-wide,
// so only the first colon-delimited segment is ever compared.
func deviceUDIDFromEnrollmentID(enrollmentID string) string {
	udid, _, _ := strings.Cut(enrollmentID, ":")
	return udid
}

// outerDeviceUDID returns the device UDID the MDM server authenticated for an event,
// preferring its udid field and falling back to the device segment of its enrollment ID.
func outerDeviceUDID(udid, enrollmentID string) string {
	if udid != "" {
		return deviceUDIDFromEnrollmentID(udid)
	}
	return deviceUDIDFromEnrollmentID(enrollmentID)
}

// bindDeviceToEnrollment binds a device decoded from the raw payload to the identity
// the MDM server authenticated.
//
// The raw payload is device-controlled and reaches UpdateDevice, which persists every
// non-zero field, so a payload naming another device could write that device's row.
// A raw UDID that disagrees with the outer identity is refused outright. An empty raw
// UDID is FORCED to the outer value rather than left blank, because UpdateDevice falls
// back to a serial-number lookup when the UDID is empty, which would let a payload
// reach a row by serial alone.
//
// When the MDM server sends no outer identity at all (older MicroMDM events) there is
// nothing to bind to, so the event is passed through and counted.
func bindDeviceToEnrollment(device *types.Device, udid, enrollmentID string) error {
	outer := outerDeviceUDID(udid, enrollmentID)
	if outer == "" {
		WarnLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      "Webhook event carries no outer enrollment identity; cannot bind the raw payload to a device",
		})
		return nil
	}

	if device.UDID == "" {
		device.UDID = outer
		return nil
	}

	if device.UDID != outer {
		ErrorLogger(LogHolder{
			DeviceUDID:   outer,
			DeviceSerial: device.SerialNumber,
			Message:      "Raw payload claims UDID " + device.UDID + ", which is not the enrolled device; refusing the event",
		})
		return errors.Errorf("raw payload UDID %v does not match enrollment %v", device.UDID, outer)
	}

	return nil
}

func handleCheckinEvent(topic string, event *types.CheckinEvent) error {
	var device types.Device
	if err := plist.Unmarshal(event.RawPayload, &device); err != nil {
		return errors.Wrap(err, "handleCheckinEvent:plist.Unmarshal")
	}

	if err := bindDeviceToEnrollment(&device, event.UDID, event.EnrollmentID); err != nil {
		return errors.Wrap(err, "handleCheckinEvent:bindDeviceToEnrollment")
	}

	// Migration rollback safety net: mirror this checkin into MicroMDM (fire-and-forget)
	syncCheckinToMicroMDM(topic, event.RawPayload)

	if isUserChannel(event.RawPayload) {
		DebugLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Not applying user-channel " + topic + " to the device"})
		return nil
	}

	if topic == "mdm.CheckOut" {
		if err := ResetDevice(device); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
			return err
		}
		return nil
	}

	device.Active = true
	oldBuild := previousBuildVersion(device.UDID)
	newBuild := device.BuildVersion

	switch topic {
	case "mdm.Authenticate":
		if err := ResetDevice(device); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
			return err
		}
	case "mdm.TokenUpdate":
		if _, err := SetTokenUpdate(device); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
			return err
		}
	}

	currentDevice, err := UpdateDevice(device)
	if err != nil {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
		return err
	}

	if err := reconcileDeviceState(currentDevice); err != nil {
		return err
	}

	if err := pushOnNewBuild(*currentDevice, oldBuild, newBuild); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
		return err
	}

	return nil
}

func handleAcknowledgeEvent(event *types.AcknowledgeEvent) error {
	var device types.Device
	if err := plist.Unmarshal(event.RawPayload, &device); err != nil {
		return errors.Wrap(err, "handleAcknowledgeEvent:plist.Unmarshal")
	}

	if err := bindDeviceToEnrollment(&device, event.UDID, event.EnrollmentID); err != nil {
		return errors.Wrap(err, "handleAcknowledgeEvent:bindDeviceToEnrollment")
	}

	var payloadDict map[string]interface{}
	if err := plist.Unmarshal(event.RawPayload, &payloadDict); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
		return err
	}

	device.Active = true
	// One lookup serves both the build comparison and the Idle gate below: the gate
	// has to know whether the row existed BEFORE UpdateDevice, which creates it.
	existingDevice, existingErr := GetDevice(device.UDID)
	deviceKnown := existingErr == nil
	oldBuild := ""
	if deviceKnown {
		oldBuild = existingDevice.BuildVersion
	}
	newBuild := device.BuildVersion

	currentDevice, err := UpdateDevice(device)
	if err != nil {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
		return err
	}

	if err := reconcileDeviceState(currentDevice); err != nil {
		return err
	}

	if err := pushOnNewBuild(*currentDevice, oldBuild, newBuild); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
		return err
	}

	if event.CommandUUID != "" {
		if err := UpdateCommand(event, device, payloadDict); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
			return err
		}
	}

	if event.Status == "Idle" {
		// A freshly enrolled device goes Idle before RunInitialTasks has sent it
		// anything. Requesting its info now would get a ProfileList back while the
		// previous enrollment's DDM declarations are still being cleared, and
		// VerifyMDMProfiles would push into that window. RunInitialTasks requests the
		// same info itself once the cleanup is done. A device whose TokenUpdate was
		// lost stays here until the next TokenUpdate or the unconfigured-devices sweep
		// picks it up, so this is logged at info and counted.
		if !idleSideEffectAllowed(event, device, deviceKnown) {
			return nil
		}
		if initialTasksPending(*currentDevice, deferPathIdleInfoRequest) {
			return nil
		}
		// currentDevice, not device: the acknowledge plist carries no serial number,
		// and RequestAllDeviceInfo uses the serial to skip re-reading the row per command.
		RequestDeviceUpdate(*currentDevice)
		return nil
	}

	if err := processAcknowledgePayload(event, *currentDevice, payloadDict); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
		return err
	}
	return nil
}

func processAcknowledgePayload(event *types.AcknowledgeEvent, device types.Device, payloadDict map[string]interface{}) error {
	if _, ok := payloadDict["ProfileList"]; ok {
		InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Received ProfileList payload"})
		var profileListData types.ProfileListData
		if err := plist.Unmarshal(event.RawPayload, &profileListData); err != nil {
			return errors.Wrap(err, "processAcknowledgePayload:ProfileList:plist.Unmarshal")
		}

		jsonBlob, err := profileListDataJSON(profileListData)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		} else {
			DebugLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "ProfileList Data", Metric: string(jsonBlob)})
		}

		if err := VerifyMDMProfiles(profileListData, device); err != nil {
			return errors.Wrap(err, "processAcknowledgePayload:VerifyMDMProfiles")
		}
		return device.UpdateLastProfileList()
	}

	if _, ok := payloadDict["SecurityInfo"]; ok {
		InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Received SecurityInfo payload"})
		var securityInfoData types.SecurityInfoData
		if err := plist.Unmarshal(event.RawPayload, &securityInfoData); err != nil {
			return errors.Wrap(err, "processAcknowledgePayload:SecurityInfo:plist.Unmarshal")
		}
		return SaveSecurityInfo(securityInfoData, device)
	}

	if _, ok := payloadDict["CertificateList"]; ok {
		InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Received CertificateList payload"})
		var certificateListData types.CertificateListData
		if err := plist.Unmarshal(event.RawPayload, &certificateListData); err != nil {
			return errors.Wrap(err, "processAcknowledgePayload:CertificateList:plist.Unmarshal")
		}
		if err := processCertificateList(certificateListData, device); err != nil {
			return errors.Wrap(err, "processAcknowledgePayload:processCertificateList")
		}
		return device.UpdateLastCertificateList()
	}

	if _, ok := payloadDict["QueryResponses"]; ok {
		InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Received DeviceInformation.QueryResponses payload"})
		var deviceInformationQueryResponses types.DeviceInformationQueryResponses
		if err := plist.Unmarshal(event.RawPayload, &deviceInformationQueryResponses); err != nil {
			return errors.Wrap(err, "processAcknowledgePayload:QueryResponses:plist.Unmarshal")
		}
		if _, err := UpdateDevice(deviceInformationQueryResponses.QueryResponses); err != nil {
			return errors.Wrap(err, "processAcknowledgePayload:UpdateDeviceInfo")
		}
		// Only a real DeviceInformation response carries IsSupervised et al.;
		// UpdateDevice no longer writes them itself so that every other
		// checkin/acknowledge type (which lack these fields entirely) can't
		// clobber a correct value back to false.
		if err := UpdateDeviceBools(&deviceInformationQueryResponses.QueryResponses); err != nil {
			return errors.Wrap(err, "processAcknowledgePayload:UpdateDeviceBools")
		}
		return device.UpdateLastDeviceInfo()
	}

	return nil
}

// idleSideEffectAllowed reports whether an Idle command report may drive
// RequestDeviceUpdate, which also checks the lock and erase flags and so can enqueue
// DeviceLock or EraseDevice.
//
// The gate is "a device row already existed before this event", NOT "the report
// quotes a known command". A legitimate Idle report carries an EMPTY command UUID:
// the device sends Idle precisely when it has nothing to report, and NanoMDM's own
// queue end-to-end test reports Idle with an empty UUID against both an empty and a
// full queue. Requiring a command UUID would therefore break the periodic info
// refresh for the whole fleet. Requiring the row to pre-exist still closes the
// single-request attack, where one event creates a device row carrying Lock, Erase
// and UnlockPin out of the raw payload and then trips the side effect in the same
// call.
//
// A non-empty command UUID is never sent with Idle by a real device, so when one is
// present it must at least belong to a command of this device.
func idleSideEffectAllowed(event *types.AcknowledgeEvent, device types.Device, deviceKnown bool) bool {
	if !deviceKnown {
		WarnLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      "Idle report for a device with no pre-existing record; not requesting a device update",
		})
		if utils.Prometheus() {
			metrics.WebhookIdleRejected("unknown_device").Inc()
		}
		return false
	}

	if event.CommandUUID == "" {
		return true
	}

	var command types.Command
	if err := db.DB.Model(&command).
		Select("command_uuid").
		Where("command_uuid = ? AND device_ud_id = ?", event.CommandUUID, device.UDID).
		First(&command).Error; err != nil {
		WarnLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      "Idle report quotes command " + event.CommandUUID + ", which is not a command of this device; not requesting a device update",
		})
		if utils.Prometheus() {
			metrics.WebhookIdleRejected("foreign_command").Inc()
		}
		return false
	}

	return true
}

// minInfoRequestClaimTTL is the shortest claim a won info request may hold. A claim
// shorter than this is written already expired or near enough to it that a concurrent
// check-in claims the same device, which is the race the claim exists to close.
const minInfoRequestClaimTTL = 1

func RequestDeviceUpdate(device types.Device) {
	var deviceModel types.Device
	// Checking for device lock or wipe
	if err := db.DB.Model(&deviceModel).Select("ud_id", "erase", "lock", "serial_number").Where("lock = ? AND ud_id = ?", true, device.UDID).Or("erase = ? AND ud_id = ?", true, device.UDID).First(&device).Error; err == nil {
		err = EraseLockDevice(device.UDID)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}

	}

	now := time.Now()
	intervalMins := utils.InfoRequestInterval()
	interval := now.Add(time.Minute * time.Duration(-intervalMins))

	// Claim before any enqueue so concurrent Idle webhooks across replicas share one
	// batch. NULL is the never-requested state.
	//
	// The claim is backdated so it expires after the claim TTL rather than after the
	// whole interval. RequestAllDeviceInfo stamps last_info_requested with the current
	// time once all four commands are enqueued, which extends the hold to the full
	// interval. A batch that fails partway, or a replica that dies mid-batch, therefore
	// frees the device after the TTL instead of making it wait out the interval with
	// stale inventory. A deferred release cannot cover process death, so the TTL is the
	// recovery path, in the same shape as the run_initial_tasks_starttime lease.
	// The TTL is clamped at both ends. Startup rejects a TTL below one minute, but the
	// gate does not rely on that: flags can be set directly, and a stamp written outside
	// this range is indistinguishable from a healthy claim once it is in the column.
	//
	// Above the interval the claim cannot expire early, so it falls back to holding the
	// whole interval. At or below zero the stamp would be written already expired, which
	// silently turns the gate off and lets every replica send the batch; one minute is
	// the shortest hold that still excludes a concurrent check-in.
	ttlMins := utils.InfoRequestClaimTTL()
	if ttlMins >= intervalMins {
		ttlMins = intervalMins
	}
	if ttlMins < minInfoRequestClaimTTL {
		ttlMins = minInfoRequestClaimTTL
	}
	claimStamp := now.Add(time.Minute * time.Duration(ttlMins-intervalMins))

	res := db.DB.Exec(`
		UPDATE devices
		SET    last_info_requested = ?
		WHERE  ud_id = ?
		  AND  ( last_info_requested IS NULL OR last_info_requested < ? )
	`, claimStamp, device.UDID, interval)
	if res.Error != nil {
		metrics.InfoRequestClaims("error").Inc()
		ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: errors.Wrap(res.Error, "RequestDeviceUpdate:claim info request").Error()})
		return
	}
	if res.RowsAffected == 1 {
		metrics.InfoRequestClaims("claimed").Inc()
		log.Debugf("Requesting Update device due to idle response from device %v", device.UDID)
		err := RequestAllDeviceInfo(device)
		if err != nil {
			metrics.InfoRequestClaims("failed").Inc()
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}
	} else {
		metrics.InfoRequestClaims("contended").Inc()
		InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Not pushing, info request not claimed within info-request-interval"})
	}

	// PushDevice(device.UDID)
}

// previousBuildVersion returns the BuildVersion currently persisted for the given UDID
// returns "" if the device is unknown (first enrollment) or the row can't be loaded
func previousBuildVersion(udid string) string {
	existing, err := GetDevice(udid)
	if err != nil {
		return ""
	}
	return existing.BuildVersion
}

// pushOnNewBuild re-pushes all profiles when the device's BuildVersion has moved forward (e.g. macOS upgrade).
//
// It does nothing while the device's initial tasks are pending. RunInitialTasks pushes
// everything anyway, and until it has cleared the previous enrollment's DDM declarations
// (resetDDMForEnrollment) a push from here would re-associate the device's KMFDDM set and
// notify, letting the device sync declarations that are about to be removed. An erase and
// reinstall onto a newer build is exactly when both would fire together.
func pushOnNewBuild(device types.Device, oldBuild string, newBuild string) error {
	if !utils.PushOnNewBuild() {
		return nil
	}
	if device.UDID == "" {
		return errors.Wrap(fmt.Errorf("device does not have a udid set"), "No Device UDID set")
	}
	if oldBuild == "" || newBuild == "" {
		return nil
	}

	oldVersion, err := version.NewVersion(oldBuild)
	if err != nil {
		return err
	}
	newVersion, err := version.NewVersion(newBuild)
	if err != nil {
		return err
	}
	if !oldVersion.LessThan(newVersion) {
		return nil
	}
	if initialTasksPending(device, deferPathPushOnNewBuild) {
		return nil
	}

	if _, err := InstallAllProfiles(device); err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
	return nil
}
