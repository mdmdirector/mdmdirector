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
	} else {
		device.Active = true
	}

	UpdateDevice(device)

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

	}

}
