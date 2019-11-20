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
	"github.com/mdmdirector/mdmdirector/log"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
)

func WebhookHandler(w http.ResponseWriter, r *http.Request) {
	var out types.PostPayload
	var err error
	err = json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		log.Error(err)
	}

	var device types.Device

	if out.CheckinEvent != nil {
		err = plist.Unmarshal(out.CheckinEvent.RawPayload, &device)
		if err != nil {
			log.Error(err)
		}
	} else if out.AcknowledgeEvent != nil {
		err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &device)
		if err != nil {
			log.Error(err)
		}
	}

	if out.Topic == "mdm.CheckOut" {
		device.Active = false
		device.AuthenticateRecieved = false
		device.TokenUpdateRecieved = false
		device.InitialTasksRun = false
		err = ClearCommands(&device)
		if err != nil {
			log.Error(err)
		}
	} else {
		device.Active = true
	}

	if out.Topic == "mdm.Authenticate" {
		err = ResetDevice(device)
		if err != nil {
			log.Error(err)
		}
	} else if out.Topic == "mdm.TokenUpdate" {
		tokenUpdateDevice, err := SetTokenUpdate(device)
		if err != nil {
			log.Error(err)
		}

		if !tokenUpdateDevice.InitialTasksRun {
			_, err := UpdateDevice(device)
			if err != nil {
				log.Error(err)
			}
			log.Error("Running initial tasks due to device update")
			RunInitialTasks(device.UDID)
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
		log.Error(err)
	}

	if !updatedDevice.InitialTasksRun && updatedDevice.TokenUpdateRecieved {
		log.Error("Running initial tasks due to device update")
		RunInitialTasks(device.UDID)
		return
	}

	if utils.PushOnNewBuild() {
		err = pushOnNewBuild(oldUDID, oldBuild)
		if err != nil {
			log.Info(err)
		}
	}

	if out.AcknowledgeEvent != nil {

		err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &device)
		if err != nil {
			log.Error(err)
		}
		if out.AcknowledgeEvent.CommandUUID != "" {
			UpdateCommand(out.AcknowledgeEvent, device)
		}

		if out.AcknowledgeEvent.Status == "Idle" {
			RequestDeviceUpdate(device)
			return
		}
		var payloadDict map[string]interface{}
		err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &payloadDict)
		if err != nil {
			log.Error(err)
		}

		// utils.PrintStruct(payloadDict)

		// Is this a ProfileList response?
		_, ok := payloadDict["ProfileList"]
		if ok {
			var profileListData types.ProfileListData
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &profileListData)
			if err != nil {
				log.Error(err)
			}
			VerifyMDMProfiles(profileListData, device)
		}

		_, ok = payloadDict["SecurityInfo"]
		if ok {
			var securityInfoData types.SecurityInfoData
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &securityInfoData)
			if err != nil {
				log.Error(err)
			}
			SaveSecurityInfo(securityInfoData, device)
		}

		_, ok = payloadDict["DeviceInformation"]
		if ok {
			var securityInfoData types.SecurityInfoData
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &securityInfoData)
			if err != nil {
				log.Error(err)
			}
			SaveSecurityInfo(securityInfoData, device)
		}

		_, ok = payloadDict["CertificateList"]
		if ok {
			var certificateListData types.CertificateListData
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &certificateListData)
			if err != nil {
				log.Error(err)
				return
			}
			err = processCertificateList(certificateListData, device)
			if err != nil {
				log.Error(err)
				return
			}
		}

		_, ok = payloadDict["QueryResponses"]
		if ok {
			var deviceInformationQueryResponses types.DeviceInformationQueryResponses
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &deviceInformationQueryResponses)
			if err != nil {
				log.Error(err)
			}
			// utils.PrintStruct(deviceInformationQueryResponses.QueryResponses)
			UpdateDevice(deviceInformationQueryResponses.QueryResponses)

		}

	}
}

func RequestDeviceUpdate(device types.Device) {
	var deviceModel types.Device
	var err error

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
		log.Error(err)
	}
	log.Debugf("Requesting Update device due to idle response from device %v", device.UDID)
	RequestProfileList(device)
	RequestSecurityInfo(device)
	RequestDeviceInformation(device)
	RequestCertificateList(device)

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
				InstallAllProfiles(oldDevice)
			}
		}
	}

	return nil
}
