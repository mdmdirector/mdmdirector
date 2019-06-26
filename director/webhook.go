package director

import (
	"encoding/json"
	"fmt"
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

		_, ok := payloadDict["china"]
		if ok {
			fmt.Println("Key found value is: ", value)
		} else {
			fmt.Println("Key not found")
		}

		// fmt.Print(temp)
		fmt.Print(string(out.AcknowledgeEvent.RawPayload))
	}

}
