package director

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/groob/plist"
	"github.com/hashicorp/go-version"
	"github.com/mdmdirector/mdmdirector/db"
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
		if err := handleCheckinEvent(out.Topic, out.CheckinEvent); err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
		return
	}

	if out.AcknowledgeEvent != nil {
		if err := handleAcknowledgeEvent(out.AcknowledgeEvent); err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
	}
}

// reconcileDeviceState handles post-enrollment lifecycle transitions after any device event
// Returns (true, nil) if RunInitialTasks was triggered — caller must return immediately
// Returns (false, err) if SendDeviceConfigured failed — caller must propagate the error
func reconcileDeviceState(device types.Device, currentDevice *types.Device) (bool, error) {
	if !currentDevice.InitialTasksRun && currentDevice.TokenUpdateRecieved {
		InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Running initial tasks"})
		if err := RunInitialTasks(device.UDID); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
			return true, err
		}
		return true, nil
	}

	if currentDevice.AwaitingConfiguration && currentDevice.InitialTasksRun {
		if err := SendDeviceConfigured(*currentDevice); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
			return false, err
		}
	}

	return false, nil
}

func handleCheckinEvent(topic string, event *types.CheckinEvent) error {
	var device types.Device
	if err := plist.Unmarshal(event.RawPayload, &device); err != nil {
		return errors.Wrap(err, "handleCheckinEvent:plist.Unmarshal")
	}

	if topic == "mdm.CheckOut" {
		if err := ResetDevice(device); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
			return err
		}
		return nil
	}

	device.Active = true
	oldBuild := device.BuildVersion

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

	if done, err := reconcileDeviceState(device, currentDevice); done || err != nil {
		return err
	}

	if utils.PushOnNewBuild() {
		if err := pushOnNewBuild(device.UDID, oldBuild); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
			return err
		}
	}

	return nil
}

func handleAcknowledgeEvent(event *types.AcknowledgeEvent) error {
	var device types.Device
	if err := plist.Unmarshal(event.RawPayload, &device); err != nil {
		return errors.Wrap(err, "handleAcknowledgeEvent:plist.Unmarshal")
	}

	var payloadDict map[string]interface{}
	if err := plist.Unmarshal(event.RawPayload, &payloadDict); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
		return err
	}

	device.Active = true
	currentDevice, err := UpdateDevice(device)
	if err != nil {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
		return err
	}

	if done, err := reconcileDeviceState(device, currentDevice); done || err != nil {
		return err
	}

	oldBuild := device.BuildVersion
	if utils.PushOnNewBuild() {
		if err := pushOnNewBuild(device.UDID, oldBuild); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
			return err
		}
	}

	if event.CommandUUID != "" {
		if err := UpdateCommand(event, device, payloadDict); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
			return err
		}
	}

	if event.Status == "Idle" {
		RequestDeviceUpdate(device)
		return nil
	}

	if err := processAcknowledgePayload(event, device, payloadDict); err != nil {
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
		return device.UpdateLastDeviceInfo()
	}

	return nil
}

func RequestDeviceUpdate(device types.Device) {
	var deviceModel types.Device
	var err error
	// Checking for device lock or wipe
	if err = db.DB.Model(&deviceModel).Select("ud_id", "erase", "lock", "serial_number").Where("lock = ? AND ud_id = ?", true, device.UDID).Or("erase = ? AND ud_id = ?", true, device.UDID).First(&device).Error; err == nil {
		err = EraseLockDevice(device.UDID)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}

	}

	err = db.DB.Model(&deviceModel).Select("ud_id", "last_info_requested", "lock", "serial_number").Where("ud_id = ?", device.UDID).First(&device).Error

	if err != nil {
		ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
	}

	now := time.Now()
	intervalMins := utils.InfoRequestInterval()
	interval := now.Add(time.Minute * time.Duration(-intervalMins))
	if device.LastInfoRequested.Before(interval) {
		log.Debugf("Requesting Update device due to idle response from device %v", device.UDID)
		err = RequestAllDeviceInfo(device)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}
	} else {
		InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Not pushing, last push is within info-request-interval", Metric: device.LastInfoRequested.String()})
	}

	// PushDevice(device.UDID)
}

func pushOnNewBuild(udid string, currentBuild string) error {
	// Only compare if there is actually a build version set
	var err error
	if udid == "" {
		err = fmt.Errorf("device does not have a udid set %v", udid)
		return errors.Wrap(err, "No Device UDID set")
	}

	oldDevice, err := GetDevice(udid)
	if err != nil {
		return errors.Wrap(err, "push on new build")
	}
	if oldDevice.BuildVersion != "" {
		if currentBuild != "" {
			oldVersion, err := version.NewVersion(oldDevice.BuildVersion)
			if err != nil {
				return err
			}
			currentVersion, err := version.NewVersion(currentBuild)
			if err != nil {
				return err
			}

			if oldVersion.LessThan(currentVersion) {
				_, err = InstallAllProfiles(oldDevice)
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}
			}
		}
	}

	return nil
}
