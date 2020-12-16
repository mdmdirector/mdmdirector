package director

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/groob/plist"
	"github.com/hashicorp/go-version"
	"github.com/jinzhu/gorm"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

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
		device.Active = false
		device.AuthenticateRecieved = false
		device.TokenUpdateRecieved = false
		device.InitialTasksRun = false
		err = ClearCommands(&device)
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
				log.Error(err)
			}
			log.Info("Running initial tasks due to device update")
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
		log.Info("Running initial tasks due to device update")
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

		if out.AcknowledgeEvent.CommandUUID != "" {
			err = UpdateCommand(out.AcknowledgeEvent, device)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			}
		}

		if out.AcknowledgeEvent.Status == "Idle" {
			RequestDeviceUpdate(device)
			return
		}
		var payloadDict map[string]interface{}
		err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &payloadDict)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}

		// Is this a ProfileList response?
		_, ok := payloadDict["ProfileList"]
		if ok {
			InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Recieved ProfileList payload"})
			var profileListData types.ProfileListData
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &profileListData)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
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
			InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Recieved SecurityInfo payload"})
			var securityInfoData types.SecurityInfoData
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &securityInfoData)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			}
			err = SaveSecurityInfo(securityInfoData, device)
			if err != nil {
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
			}

			if err == nil {
				siErr := device.UpdateLastSecurityInfo()
				if siErr != nil {
					ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: siErr.Error()})
				}
			}
		}

		_, ok = payloadDict["CertificateList"]
		if ok {
			var certificateListData types.CertificateListData
			InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Recieved CertificateList payload"})
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
			InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Recieved DeviceInformation.QueryResponses payload"})
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
	if err = db.DB.Model(&deviceModel).Where("lock = ? AND ud_id = ?", true, device.UDID).Or("erase = ? AND ud_id = ?", true, device.UDID).First(&device).Error; err == nil {
		err = EraseLockDevice(device.UDID)
		if err != nil {
			ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
		}

	}

	// hourAgo := time.Now().Add(1 * time.Hour)
	thirtyMinsAgo := time.Now().Add(-30 * time.Minute)

	if utils.DebugMode() {
		thirtyMinsAgo = time.Now().Add(-5 * time.Minute)
	}

	if err = db.DB.Model(&deviceModel).Where("last_info_requested < ? AND ud_id = ?", thirtyMinsAgo, device.UDID).First(&device).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			log.Debug("Last updated was under 30 minutes ago")
			return
		}
	}

	err = db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"last_info_requested": time.Now()}).Error
	if err != nil {
		ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
	}
	log.Debugf("Requesting Update device due to idle response from device %v", device.UDID)
	err = RequestAllDeviceInfo(device)
	if err != nil {
		ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: err.Error()})
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
					log.Error(err)
				}
			}
		}
	}

	return nil
}
