package director

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/grahamgilbert/mdmdirector/types"
	"github.com/groob/plist"
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
