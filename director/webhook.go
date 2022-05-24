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

func profileListDataJSON(device types.Device, profileListData types.ProfileListData) ([]byte, error) {
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

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}

	var device types.Device

	if out.CheckinEvent != nil {
		err = plist.Unmarshal(out.CheckinEvent.RawPayload, &device)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}
	} else if out.AcknowledgeEvent != nil {
		err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &device)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}
	}

	if out.Topic == "mdm.CheckOut" {
		err = ResetDevice(device)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}
	} else {
		device.Active = true
	}

	if out.Topic == "mdm.Authenticate" {
		err = ResetDevice(device)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}
	} else if out.Topic == "mdm.TokenUpdate" {
		tokenUpdateDevice, err := SetTokenUpdate(device)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}

		if !tokenUpdateDevice.InitialTasksRun {
			_, err := UpdateDevice(device)
			if err != nil {
				ErrorLogger(LogHolder{Message: err.Error()})
			}
			InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Running initial tasks due to device update"})
			err = RunInitialTasks(device.UDID)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			}
			return
		}
	}
	oldUDID := device.UDID
	oldBuild := device.BuildVersion
	if device.UDID == "" {
		log.Error(out)
		log.Fatal("No device UDID set")
	}
	updatedDevice, err := UpdateDevice(device)
	if err != nil {
		ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
	}

	if !updatedDevice.InitialTasksRun && updatedDevice.TokenUpdateRecieved {
		InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Running initial tasks due to device update"})
		err = RunInitialTasks(device.UDID)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}
		return
	}

	if utils.PushOnNewBuild() {
		err = pushOnNewBuild(oldUDID, oldBuild)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}
	}

	if out.AcknowledgeEvent != nil {

		var payloadDict map[string]interface{}
		err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &payloadDict)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}

		if out.AcknowledgeEvent.CommandUUID != "" {
			err = UpdateCommand(out.AcknowledgeEvent, device, payloadDict)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			}
		}

		if out.AcknowledgeEvent.Status == "Idle" {
			RequestDeviceUpdate(device)
			return
		}

		// Is this a ProfileList response?
		_, ok := payloadDict["ProfileList"]
		if ok {
			lh := LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Received ProfileList payload"}
			InfoLogger(lh)
			var profileListData types.ProfileListData
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &profileListData)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			}
			jsonBlob, err := profileListDataJSON(device, profileListData)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			} else {
				DebugLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "ProfileList Data", Metric: string(jsonBlob)})
			}

			err = VerifyMDMProfiles(profileListData, device)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			}

			if err == nil {
				plErr := device.UpdateLastProfileList()
				if plErr != nil {
					ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: plErr.Error()})
				}
			}
		}

		_, ok = payloadDict["SecurityInfo"]
		if ok {
			InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Received SecurityInfo payload"})
			var securityInfoData types.SecurityInfoData
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &securityInfoData)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			}
			err = SaveSecurityInfo(securityInfoData, device)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			}
		}

		_, ok = payloadDict["CertificateList"]
		if ok {
			var certificateListData types.CertificateListData
			InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Received CertificateList payload"})
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &certificateListData)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			}
			err = processCertificateList(certificateListData, device)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			}

			if err == nil {
				clErr := device.UpdateLastCertificateList()
				if clErr != nil {
					ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: clErr.Error()})
				}
			}
		}

		_, ok = payloadDict["QueryResponses"]
		if ok {
			InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Received DeviceInformation.QueryResponses payload"})
			var deviceInformationQueryResponses types.DeviceInformationQueryResponses
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &deviceInformationQueryResponses)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			}
			_, err = UpdateDevice(deviceInformationQueryResponses.QueryResponses)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			}

			if err == nil {
				diErr := device.UpdateLastDeviceInfo()
				if diErr != nil {
					ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: diErr.Error()})
				}
			}
		}
	}
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
		err = fmt.Errorf("Device does not have a udid set %v", udid)
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
