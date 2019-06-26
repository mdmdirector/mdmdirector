package director

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/types"
	"github.com/groob/plist"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

func ProfileHandler(w http.ResponseWriter, r *http.Request) {
	var profiles []types.DeviceProfile
	var devices []types.Device
	var out types.ProfilePayload

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		log.Print(err)
	}

	for _, payload := range out.Mobileconfigs {
		var profile types.DeviceProfile
		mobileconfig, err := base64.StdEncoding.DecodeString(string(payload))
		if err != nil {
			log.Print(err)
		}
		err = plist.Unmarshal(mobileconfig, &profile)
		if err != nil {
			log.Print(err)
		}

		profile.MobileconfigData = mobileconfig
		mobileconfigData := []byte(mobileconfig)
		hash := sha256.Sum256(mobileconfigData)
		profile.MobileconfigHash = hash[:]

		profiles = append(profiles, profile)
	}

	if out.DeviceUUIDs != nil {
		// Not empty list
		if len(out.DeviceUUIDs) > 0 {
			// Targeting all devices
			if out.DeviceUUIDs[0] == "*" {
				fmt.Println("This will hit all devices")
			} else {
				for _, item := range out.DeviceUUIDs {
					device := GetDevice(item)
					devices = append(devices, device)
				}
			}

			ProcessProfiles(devices, profiles)
		}

	} else if out.SerialNumbers != nil {
		if len(out.SerialNumbers) > 0 {
			// Targeting all devices
			if out.SerialNumbers[0] == "*" {
				fmt.Println("This will hit all devices")
			} else {
				for _, item := range out.SerialNumbers {
					device := GetDeviceSerial(item)
					devices = append(devices, device)
				}
			}
			ProcessProfiles(devices, profiles)
		}

	}
}

func ProcessProfiles(devices []types.Device, profiles []types.DeviceProfile) {
	var profile types.DeviceProfile
	for _, device := range devices {

		tx := db.DB.Model(&profile).Where("device_ud_id = ?", device.UDID)

		for _, profileData := range profiles {
			tx = tx.Where("payload_identifier = ?", profileData.PayloadIdentifier)
		}
		tx.Delete(&profile)
		db.DB.Model(&device).Association("Profiles").Append(profiles)

		for _, profileData := range profiles {
			var commandPayload types.CommandPayload
			// var jsonString []byte
			commandPayload.UDID = device.UDID
			commandPayload.RequestType = "InstallProfile"
			// Next job: sign this
			commandPayload.Payload = base64.StdEncoding.EncodeToString([]byte(profileData.MobileconfigData))

			SendCommand(commandPayload)

		}
	}
}
