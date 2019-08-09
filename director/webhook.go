package director

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/types"
	"github.com/grahamgilbert/mdmdirector/utils"
	"github.com/groob/plist"
	"github.com/jinzhu/gorm"
)

func WebhookHandler(w http.ResponseWriter, r *http.Request) {
	var out types.PostPayload
	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		log.Print(err)
	}

	var device types.Device

	if out.CheckinEvent != nil {
		err = plist.Unmarshal(out.CheckinEvent.RawPayload, &device)
		if err != nil {
			log.Print(err)
		}

	}

	if out.Topic == "mdm.CheckOut" {
		device.Active = false
		device.AuthenticateRecieved = false
		device.TokenUpdateRecieved = false
		device.InitialTasksRun = false
		ClearCommands(&device)
	} else {
		device.Active = true
	}

	if out.Topic == "mdm.Authenticate" {
		ResetDevice(device)
	} else if out.Topic == "mdm.TokenUpdate" {
		SetTokenUpdate(device)
	}

	updatedDevice := UpdateDevice(device)
	if updatedDevice.InitialTasksRun == false && updatedDevice.TokenUpdateRecieved == true {
		log.Print("Running initial tasks due to device update")
		RunInitialTasks(device.UDID)
	}

	// if device.TokenUpdateRecieved == false {
	// 	return
	// }

	if out.AcknowledgeEvent != nil {

		err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &device)
		if err != nil {
			log.Print(err)
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
			log.Print(err)
		}

		// utils.PrintStruct(payloadDict)

		// Is this a ProfileList response?
		_, ok := payloadDict["ProfileList"]
		if ok {
			var profileListData types.ProfileListData
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &profileListData)
			if err != nil {
				log.Print(err)
			}
			VerifyMDMProfiles(profileListData, device)
		}

		_, ok = payloadDict["SecurityInfo"]
		if ok {
			var securityInfoData types.SecurityInfoData
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &securityInfoData)
			if err != nil {
				log.Print(err)
			}
			SaveSecurityInfo(securityInfoData, device)
		}

		_, ok = payloadDict["DeviceInformation"]
		if ok {
			var securityInfoData types.SecurityInfoData
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &securityInfoData)
			if err != nil {
				log.Print(err)
			}
			SaveSecurityInfo(securityInfoData, device)
		}

		_, ok = payloadDict["QueryResponses"]
		if ok {
			var deviceInformationQueryResponses types.DeviceInformationQueryResponses
			err = plist.Unmarshal(out.AcknowledgeEvent.RawPayload, &deviceInformationQueryResponses)
			if err != nil {
				log.Print(err)
			}
			UpdateDevice(deviceInformationQueryResponses.QueryResponses)
		}

	}

}

func RequestDeviceUpdate(device types.Device) {
	var deviceModel types.Device

	// hourAgo := time.Now().Add(1 * time.Hour)
	thirtyMinsAgo := time.Now().Add(-30 * time.Minute)

	if utils.DebugMode() {
		thirtyMinsAgo = time.Now().Add(-5 * time.Minute)
	}

	if err := db.DB.Model(&deviceModel).Where("last_info_requested < ? AND ud_id = ?", thirtyMinsAgo, device.UDID).First(&device).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			log.Print("Last updated was under 30 minutes ago")
			return
		}
	}

	err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"last_info_requested": time.Now()}).Error
	if err != nil {
		log.Print(err)
	}
	log.Printf("Requesting Update device due to idle response from device %v", device.UDID)
	RequestProfileList(device)
	RequestSecurityInfo(device)
	RequestDeviceInformation(device)

	// PushDevice(device.UDID)
}
