package director

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"

	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/types"
	"github.com/groob/plist"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

func ProfileHandler(w http.ResponseWriter, r *http.Request) {
	var profiles []types.DeviceProfile
	var sharedProfiles []types.SharedProfile
	var devices []types.Device
	var out types.ProfilePayload

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		log.Print(err)
	}

	for _, payload := range out.Mobileconfigs {
		var profile types.DeviceProfile
		var sharedProfile types.SharedProfile
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

		err = plist.Unmarshal(mobileconfig, &sharedProfile)
		if err != nil {
			log.Print(err)
		}

		profile.MobileconfigData = mobileconfig
		sharedProfile.MobileconfigHash = hash[:]
		sharedProfiles = append(sharedProfiles, sharedProfile)
	}

	if out.DeviceUUIDs != nil {
		// Not empty list
		if len(out.DeviceUUIDs) > 0 {
			// Targeting all devices
			if out.DeviceUUIDs[0] == "*" {
				devices = GetAllDevices()
				ProcessSharedProfiles(devices, sharedProfiles)
			} else {
				for _, item := range out.DeviceUUIDs {
					device := GetDevice(item)
					devices = append(devices, device)
				}
				ProcessProfiles(devices, profiles)
			}
		}

	} else if out.SerialNumbers != nil {
		if len(out.SerialNumbers) > 0 {
			// Targeting all devices
			if out.SerialNumbers[0] == "*" {
				devices = GetAllDevices()
				ProcessSharedProfiles(devices, sharedProfiles)
			} else {
				for _, item := range out.SerialNumbers {
					device := GetDeviceSerial(item)
					devices = append(devices, device)
				}
				ProcessProfiles(devices, profiles)
			}
		}

	}
}

func ProcessProfiles(devices []types.Device, profiles []types.DeviceProfile) {
	var profile types.DeviceProfile
	for _, device := range devices {

		tx := db.DB.Model(&profile).Where("device_ud_id = ?", device.UDID)

		for _, profileData := range profiles {
			if profileData.PayloadIdentifier != "" {
				tx = tx.Where("payload_identifier = ?", profileData.PayloadIdentifier)
			}
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

func ProcessSharedProfiles(devices []types.Device, profiles []types.SharedProfile) {
	var profile types.SharedProfile
	for _, device := range devices {

		tx := db.DB.Model(&profile).Where("device_ud_id = ?", device.UDID)

		for _, profileData := range profiles {
			if profileData.PayloadIdentifier != "" {
				tx = tx.Where("payload_identifier = ?", profileData.PayloadIdentifier)
			}
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

func VerifyMDMProfiles(profileListData types.ProfileListData, device types.Device) {

	var profile types.DeviceProfile
	var profiles []types.DeviceProfile
	var sharedProfile types.SharedProfile
	var sharedProfiles []types.SharedProfile
	var profilesToInstall []types.DeviceProfile
	var sharedProfilesToInstall []types.SharedProfile
	var devices []types.Device

	// Get the profiles that should be installed on the device
	err := db.DB.Model(&profile).Where("device_ud_id = ?", device.UDID).Scan(&profiles).Error
	if err != nil {
		log.Print(err)
	}

	// For each, loop over the present profiles
	for _, savedProfile := range profiles {
		for _, incomingProfile := range profileListData.ProfileList {
			if savedProfile.PayloadUUID != incomingProfile.PayloadUUID || savedProfile.PayloadIdentifier != incomingProfile.PayloadIdentifier {
				// If missing, queue up to be installed
				profilesToInstall = append(profilesToInstall, savedProfile)
			}
		}
	}

	devices = append(devices, device)
	ProcessProfiles(devices, profilesToInstall)

	err = db.DB.Model(&sharedProfile).Where("device_ud_id = ?", device.UDID).Scan(&sharedProfiles).Error
	if err != nil {
		log.Print(err)
	}

	for _, savedSharedProfile := range sharedProfiles {
		for _, incomingProfile := range profileListData.ProfileList {
			if savedSharedProfile.PayloadUUID != incomingProfile.PayloadUUID || savedSharedProfile.PayloadIdentifier != incomingProfile.PayloadIdentifier {
				// If missing, queue up to be installed
				sharedProfilesToInstall = append(sharedProfilesToInstall, savedSharedProfile)
			}
		}
	}

	ProcessSharedProfiles(devices, sharedProfilesToInstall)

}
