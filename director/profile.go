package director

import (
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	intErrors "errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/pkcs12"

	"github.com/fullsailor/pkcs7"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/groob/plist"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func PostProfileHandler(w http.ResponseWriter, r *http.Request) {
	var profiles []types.DeviceProfile
	var sharedProfiles []types.SharedProfile
	var devices []types.Device
	var out types.ProfilePayload
	var metadata []types.MetadataItem

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}

	useMetadata := out.Metadata

	for payloadi := range out.Mobileconfigs {
		var profile types.DeviceProfile
		var sharedProfile types.SharedProfile
		payload := out.Mobileconfigs[payloadi]
		mobileconfig, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		err = plist.Unmarshal(mobileconfig, &profile)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		var tempProfileDict map[string]interface{}
		err = plist.Unmarshal(mobileconfig, &tempProfileDict)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		profile.HashedPayloadUUID = uuid.NewSHA1(uuid.NameSpaceDNS, mobileconfig).String()

		tempProfileDict["PayloadUUID"] = profile.HashedPayloadUUID

		mobileconfig, err = plist.MarshalIndent(&tempProfileDict, "\t")
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		profile.MobileconfigData = mobileconfig
		mobileconfigData := mobileconfig
		hash := sha256.Sum256(mobileconfigData)
		profile.MobileconfigHash = hash[:]

		profiles = append(profiles, profile)

		err = plist.Unmarshal(mobileconfig, &sharedProfile)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		sharedProfile.HashedPayloadUUID = uuid.NewSHA1(uuid.NameSpaceDNS, mobileconfig).String()

		var sharedTempProfileDict map[string]interface{}
		err = plist.Unmarshal(mobileconfig, &sharedTempProfileDict)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		sharedTempProfileDict["PayloadUUID"] = sharedProfile.HashedPayloadUUID

		mobileconfig, err = plist.MarshalIndent(&sharedTempProfileDict, "\t")
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		sharedProfile.MobileconfigData = mobileconfig
		sharedProfile.MobileconfigHash = hash[:]
		sharedProfiles = append(sharedProfiles, sharedProfile)
	}

	if out.DeviceUDIDs != nil {
		// Not empty list
		if len(out.DeviceUDIDs) > 0 {
			// Targeting all devices
			if out.DeviceUDIDs[0] == "*" {
				devices, err = GetAllDevices()
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				err = SaveSharedProfiles(sharedProfiles)
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}

				if out.PushNow {
					_, err = PushSharedProfiles(devices, sharedProfiles)
					if err != nil {
						ErrorLogger(LogHolder{Message: err.Error()})
					}
				}
			} else {
				// Individual devices
				for _, item := range out.DeviceUDIDs {
					device, err := GetDevice(item)
					if err != nil {
						ErrorLogger(LogHolder{Message: err.Error()})
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}
					metadataItem, err := ProcessDeviceProfiles(device, profiles, out.PushNow, "post")
					if err != nil {
						ErrorLogger(LogHolder{Message: err.Error()})
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}
					metadata = append(metadata, metadataItem)
				}
			}
		}
	} else if out.SerialNumbers != nil {
		if len(out.SerialNumbers) > 0 {
			// Targeting all devices
			if out.SerialNumbers[0] == "*" {
				devices, err = GetAllDevices()
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				err = SaveSharedProfiles(sharedProfiles)
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}

				if out.PushNow {
					_, err = PushSharedProfiles(devices, sharedProfiles)
					if err != nil {
						ErrorLogger(LogHolder{Message: err.Error()})
					}
				}
			} else {
				for _, item := range out.SerialNumbers {
					device, err := GetDeviceSerial(item)
					if err != nil {
						continue
					}
					metadataItem, err := ProcessDeviceProfiles(device, profiles, out.PushNow, "post")
					if err != nil {
						ErrorLogger(LogHolder{Message: err.Error()})
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}
					metadata = append(metadata, metadataItem)
				}
			}
		}
	}

	if useMetadata {
		output, err := json.MarshalIndent(&metadata, "", "    ")
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
		}

		_, err = w.Write(output)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
	}
}

func ProcessDeviceProfiles(device types.Device, profiles []types.DeviceProfile, pushNow bool, requestType string) (types.MetadataItem, error) {
	var metadata types.MetadataItem
	var devices []types.Device
	var profileMetadataList []types.ProfileMetadata
	var profilesToSave []types.DeviceProfile

	pushRequired := false

	// metadata.Device = device
	for i := range profiles {
		var profileMetadata types.ProfileMetadata
		status := "unchanged"
		profile := profiles[i]

		devices = append(devices, device)
		if requestType == "post" {
			profileDiffers, err := SavedDeviceProfileDiffers(device, profile)
			if err != nil {
				return metadata, errors.Wrap(err, "Could not determine if saved profile differs from incoming profile.")
			}
			profile.Installed = true
			if profileDiffers {
				profilesToSave = append(profilesToSave, profile)
				status = "changed"
				if pushNow {
					pushRequired = true
					status = "pushed"
				} else {
					status = "saved"
				}
			}

		} else if requestType == "delete" {
			profilePresent, err := SavedProfileIsPresent(device, profile)
			if err != nil {
				return metadata, errors.Wrap(err, "Could not determine if saved profile is present.")
			}

			profile.Installed = false
			profilesToSave = append(profilesToSave, profile)
			if profilePresent {
				status = "deleted"
			}

			if pushNow && profilePresent {
				pushRequired = true
			}
		}

		profileMetadata.HashedPayloadUUID = profile.HashedPayloadUUID
		profileMetadata.PayloadIdentifier = profile.PayloadIdentifier
		profileMetadata.PayloadUUID = profile.PayloadUUID
		profileMetadata.Status = status
		profileMetadataList = append(profileMetadataList, profileMetadata)

	}

	SaveProfiles(devices, profilesToSave)

	metadata.ProfileMetadata = profileMetadataList

	if pushRequired {
		err := PushDevice(device)
		if err != nil {
			return metadata, errors.Wrap(err, "Push Device")
		}

		err = setNextPushToThePast(device)
		if err != nil {
			return metadata, errors.Wrap(err, "Set last push to a date in the past")
		}
	}

	return metadata, nil
}

func SavedProfileIsPresent(device types.Device, profile types.DeviceProfile) (bool, error) {
	var savedProfile types.DeviceProfile
	// var profileList types.ProfileList
	// Make sure profile is marked as install = false
	if err := db.DB.Where("device_ud_id = ? AND payload_identifier = ? AND installed = ?", device.UDID, profile.PayloadIdentifier, false).First(&savedProfile).Error; err != nil {
		if intErrors.Is(err, gorm.ErrRecordNotFound) {
			DebugLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, ProfileIdentifier: profile.PayloadIdentifier, Message: "Profile present and marked as installed = true"})
			return true, nil
		}
	}
	// Make sure the profile isn't in the device's profilelist
	// err := db.DB.Model(&profileList).Select("device_ud_id").Where("device_ud_id = ? AND payload_identifier = ?", device.UDID, profile.PayloadIdentifier).First(&profileList).Error
	// if err != nil {
	// 	if intErrors.Is(err, gorm.ErrRecordNotFound) {
	// 		// If it's not found, we'll catch in the false return at the end. Else raise an error
	// 		DebugLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, ProfileIdentifier: profile.PayloadIdentifier, Message: "Profile not found in device's ProfileList"})
	// 		return false, nil
	// 	}

	// 	return true, errors.Wrap(err, "Could not load ProfileList for device")
	// }

	return false, nil
}

func SavedDeviceProfileDiffers(device types.Device, profile types.DeviceProfile) (bool, error) {
	var savedProfile types.DeviceProfile
	var profileList types.ProfileList
	// Profile isn't in the db
	if err := db.DB.Where("device_ud_id = ? AND payload_identifier = ? AND installed = ?", device.UDID, profile.PayloadIdentifier, true).First(&savedProfile).Error; err != nil {
		if intErrors.Is(err, gorm.ErrRecordNotFound) {
			InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileIdentifier: profile.PayloadIdentifier, ProfileUUID: profile.HashedPayloadUUID, Message: "PayloadIdentifier not found in database"})
			return true, nil
		}
	}

	// Hash doesn't match
	if savedProfile.HashedPayloadUUID != profile.HashedPayloadUUID {
		// log.Debugf("hashes do not match: saved profile %v incoming profile %v", savedProfile.HashedPayloadUUID, profile.HashedPayloadUUID)
		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileIdentifier: profile.PayloadIdentifier, ProfileUUID: profile.HashedPayloadUUID, Message: "Hashed payload UUID doesn't match what's saved", Metric: savedProfile.HashedPayloadUUID})
		return true, nil
	}

	// Profile isn't what we have saved in the profilelist
	err := db.DB.Model(&profileList).Where("device_ud_id = ? AND payload_identifier = ?", device.UDID, profile.PayloadIdentifier).Error
	if err != nil {
		if !intErrors.Is(err, gorm.ErrRecordNotFound) {
			// If it's not found, we'll catch in the false return at the end. Else raise an error
			return true, errors.Wrap(err, "Could not load ProfileList for device")
		}
	}

	if !strings.EqualFold(profileList.PayloadUUID, profile.HashedPayloadUUID) {
		// if profileList.PayloadUUID == "" {
		// 	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileIdentifier: profile.PayloadIdentifier, ProfileUUID: profile.HashedPayloadUUID, Message: "Hashed payload UUID is not present in ProfileList", Metric: profileList.PayloadUUID})
		// } else {
		// 	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileIdentifier: profile.PayloadIdentifier, ProfileUUID: profile.HashedPayloadUUID, Message: "Hashed payload UUID doesn't match what's in ProfileList", Metric: profileList.PayloadUUID})
		// }
		// // May be waiting for a device to report in full - just bail if there profilelist count is 0
		// var profileCount int
		// err := db.DB.Model(&profileList).Where("device_ud_id = ?", device.UDID).Count(&profileCount).Error
		// if err != nil {
		// 	if !intErrors.Is(err, gorm.ErrRecordNotFound) {
		// 		// If it's not found, we'll catch in the false return at the end. Else raise an error
		// 		return true, errors.Wrap(err, "Could not load ProfileList for device")
		// 	}
		// }
		// if profileCount == 0 {
		// 	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileIdentifier: profile.PayloadIdentifier, ProfileUUID: profile.HashedPayloadUUID, Message: "Device has an empty ProfileList stored"})
		// }
		// skipCommands := []string{"ProfileList", "SecurityInfo", "DeviceInformation", "CertificateList"}
		// tenMinsAgo := time.Now().Add(-10 * time.Minute)
		// for _, item := range skipCommands {
		// 	inQueue := CommandInQueue(device, item, tenMinsAgo)
		// 	if inQueue {
		// 		msg := fmt.Sprintf("%v is in queue", item)
		// 		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileIdentifier: profile.PayloadIdentifier, ProfileUUID: profile.HashedPayloadUUID, Message: msg, Metric: profileList.PayloadUUID})
		// 		return false, nil
		// 	}
		// }

		// InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileIdentifier: profile.PayloadIdentifier, ProfileUUID: profile.HashedPayloadUUID, Message: "Requesting Device Info", Metric: profileList.PayloadUUID})
		// _ = RequestAllDeviceInfo(device)

		return false, nil
	}

	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileIdentifier: profile.PayloadIdentifier, ProfileUUID: profile.HashedPayloadUUID, Message: "Profile has not changed"})
	return false, nil
}

func DisableSharedProfiles(payload types.DeleteProfilePayload) error {
	var sharedProfileModel types.SharedProfile
	var sharedProfiles []types.SharedProfile
	devices, err := GetAllDevices()
	if err != nil {
		return errors.Wrap(err, "Profiles::DisableSharedProfiles: Could not get all devices")
	}

	for _, profile := range payload.Mobileconfigs {
		err := db.DB.Model(&sharedProfileModel).Select("installed").Where("payload_identifier = ?", profile.PayloadIdentifier).Updates(map[string]interface{}{
			"installed": false,
		}).Error
		if err != nil {
			return errors.Wrap(err, "Profiles::DisableSharedProfiles: Could not set installed = false")
		}
	}
	DeleteSharedProfiles(devices, sharedProfiles)
	return nil
}

func DeleteProfileHandler(w http.ResponseWriter, r *http.Request) {
	var profiles []types.DeviceProfile
	var devices []types.Device
	var out types.DeleteProfilePayload
	var metadata []types.MetadataItem

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
	if out.DeviceUDIDs != nil {
		// Not empty list
		if len(out.DeviceUDIDs) > 0 {
			// Targeting all devices
			if out.DeviceUDIDs[0] == "*" {
				err = DisableSharedProfiles(out)
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				return
			}
			err := db.DB.Model(&devices).Where("ud_id IN (?)", out.DeviceUDIDs).Scan(&devices).Error
			if err != nil {
				ErrorLogger(LogHolder{Message: err.Error()})
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}

		}
	}
	if out.SerialNumbers != nil {
		if len(out.SerialNumbers) > 0 {
			if out.SerialNumbers[0] == "*" {
				err = DisableSharedProfiles(out)
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				return
			}
			err := db.DB.Model(&devices).Where("serial_number IN (?)", out.SerialNumbers).Scan(&devices).Error
			if err != nil {
				ErrorLogger(LogHolder{Message: err.Error()})
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}

		}
	}

	for i := range devices {
		device := devices[i]
		for i := range out.Mobileconfigs {
			var profile types.DeviceProfile
			profile.PayloadIdentifier = out.Mobileconfigs[i].PayloadIdentifier
			profiles = append(profiles, profile)
		}

		metadataItem, err := ProcessDeviceProfiles(device, profiles, out.PushNow, "delete")
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		metadata = append(metadata, metadataItem)
	}

	if out.Metadata {
		output, err := json.MarshalIndent(&metadata, "", "    ")
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
		}

		_, err = w.Write(output)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
	}

}

func SaveProfiles(devices []types.Device, profiles []types.DeviceProfile) {
	for i := range devices {
		device := devices[i]
		if device.UDID == "" {
			continue
		}
		for profilei := range profiles {
			// var profileModel types.DeviceProfile
			var boolModel types.DeviceProfile
			profileData := profiles[profilei]
			profileData.DeviceUDID = device.UDID
			err := db.DB.Clauses(clause.OnConflict{
				UpdateAll: true,
			}).Create(&profileData).Error
			if err != nil {
				theErr := fmt.Sprintf("Update profile: %v", err.Error())
				ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, ProfileIdentifier: profileData.PayloadIdentifier, Message: theErr})
			}
			// if err := db.DB.Where("device_ud_id = ? AND payload_identifier = ?", device.UDID, profileData.PayloadIdentifier).FirstOrCreate(&profileModel).Error; err != nil {
			// 	if intErrors.Is(err, gorm.ErrRecordNotFound) {
			// 		db.DB.Create(&profileData)
			// 	}
			// } else {
			// 	err := db.DB.Model(&profileModel).Where("device_ud_id = ? AND payload_identifier = ?", device.UDID, profileData.PayloadIdentifier).Assign(&profileData).FirstOrCreate(&profileModel).Error
			// 	if err != nil {
			// 		theErr := fmt.Sprintf("Update profile: %v", err.Error())
			// 		ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, ProfileIdentifier: profileData.PayloadIdentifier, Message: theErr})
			// 	}
			// }

			DebugLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, ProfileIdentifier: profileData.PayloadIdentifier, Message: "Updating profile installed bool"})
			err = db.DB.Model(&boolModel).Where("device_ud_id = ? AND payload_identifier = ?", device.UDID, profileData.PayloadIdentifier).Updates(map[string]interface{}{
				"installed": profiles[profilei].Installed,
			}).Error
			if err != nil {
				ErrorLogger(LogHolder{Message: err.Error()})
			}

		}
	}
}

func PushProfiles(devices []types.Device, profiles []types.DeviceProfile) ([]types.Command, error) {
	var pushedCommands []types.Command
	for i := range devices {
		device := devices[i]
		for i := range profiles {
			profileData := profiles[i]
			var commandPayload types.CommandPayload
			commandPayload.RequestType = "InstallProfile"

			InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Pushing Device Profile", ProfileIdentifier: profileData.PayloadIdentifier, ProfileUUID: profileData.HashedPayloadUUID, CommandRequestType: commandPayload.RequestType})

			if utils.Sign() {
				priv, pub, err := loadSigningKey(utils.KeyPassword(), utils.KeyPath(), utils.CertPath())
				if err != nil {
					log.Errorf("loading signing certificate and private key: %v", err)
				}
				signed, err := SignProfile(priv, pub, profileData.MobileconfigData)
				if err != nil {
					log.Errorf("signing profile with the specified key: %v", err)
				}

				commandPayload.Payload = base64.StdEncoding.EncodeToString(signed)
			} else {
				commandPayload.Payload = base64.StdEncoding.EncodeToString(profileData.MobileconfigData)
			}

			commandPayload.UDID = device.UDID

			command, err := SendCommand(commandPayload)
			if err != nil {
				ErrorLogger(LogHolder{Message: err.Error()})
			}
			pushedCommands = append(pushedCommands, command)

		}
	}

	return pushedCommands, nil
}

func SaveSharedProfiles(profiles []types.SharedProfile) error {
	var profile types.SharedProfile
	if len(profiles) == 0 {
		return nil
	}

	for _, profileData := range profiles {
		if profileData.PayloadIdentifier != "" {
			err := db.DB.Model(&profile).Where("payload_identifier = ?", profileData.PayloadIdentifier).Delete(&profile).Error
			if err != nil {
				ErrorLogger(LogHolder{Message: err.Error()})
				return errors.Wrap(err, "Deleting shared profiles")
			}
		}
	}

	tx2 := db.DB.Model(&profile)
	for _, profileData := range profiles {
		// utils.PrintStruct(profileData)
		err := tx2.Create(&profileData).Error
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
	}

	err := tx2.Error
	if err != nil {
		return errors.Wrap(err, "Saving shared profiles")
	}
	// db.DB.Create(&profiles)
	return nil
}

func DeleteSharedProfiles(devices []types.Device, profiles []types.SharedProfile) {
	for i := range devices {
		device := devices[i]
		for i := range profiles {
			profileData := profiles[i]
			var commandPayload types.CommandPayload
			commandPayload.UDID = device.UDID
			commandPayload.RequestType = "RemoveProfile"
			commandPayload.Identifier = profileData.PayloadIdentifier
			InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Deleting Shared Profile", ProfileIdentifier: profileData.PayloadIdentifier, ProfileUUID: profileData.HashedPayloadUUID, CommandRequestType: commandPayload.RequestType})
			_, err := SendCommand(commandPayload)
			if err != nil {
				ErrorLogger(LogHolder{Message: err.Error()})
			}
		}
	}
}

func DeleteDeviceProfiles(devices []types.Device, profiles []types.DeviceProfile) {
	for i := range devices {
		device := devices[i]
		for i := range profiles {
			profileData := profiles[i]
			var commandPayload types.CommandPayload
			commandPayload.UDID = device.UDID
			commandPayload.RequestType = "RemoveProfile"
			commandPayload.Identifier = profileData.PayloadIdentifier
			InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Deleting Device Profile", ProfileIdentifier: profileData.PayloadIdentifier, ProfileUUID: profileData.HashedPayloadUUID, CommandRequestType: commandPayload.RequestType})
			_, err := SendCommand(commandPayload)
			if err != nil {
				ErrorLogger(LogHolder{Message: err.Error()})
			}
		}
	}
}

func PushSharedProfiles(devices []types.Device, profiles []types.SharedProfile) ([]types.Command, error) {
	var pushedCommands []types.Command
	for i := range devices {
		device := devices[i]
		for i := range profiles {
			profileData := profiles[i]
			var commandPayload types.CommandPayload

			commandPayload.UDID = device.UDID
			commandPayload.RequestType = "InstallProfile"

			InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Pushing Shared Profile", ProfileIdentifier: profileData.PayloadIdentifier, ProfileUUID: profileData.HashedPayloadUUID, CommandRequestType: commandPayload.RequestType})

			if utils.Sign() {
				priv, pub, err := loadSigningKey(utils.KeyPassword(), utils.KeyPath(), utils.CertPath())
				if err != nil {
					return pushedCommands, errors.Wrap(err, "PushSharedProfiles")
				}
				signed, err := SignProfile(priv, pub, profileData.MobileconfigData)
				if err != nil {
					return pushedCommands, errors.Wrap(err, "PushSharedProfiles")
				}

				commandPayload.Payload = base64.StdEncoding.EncodeToString(signed)
			} else {
				commandPayload.Payload = base64.StdEncoding.EncodeToString(profileData.MobileconfigData)
			}

			command, err := SendCommand(commandPayload)
			if err != nil {
				return pushedCommands, errors.Wrap(err, "PushSharedProfiles")
			}

			pushedCommands = append(pushedCommands, command)

		}
	}
	return pushedCommands, nil
}

func VerifyMDMProfiles(profileListData types.ProfileListData, device types.Device) error {
	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Verifying MDM Profiles"})
	var profile types.DeviceProfile
	var profiles []types.DeviceProfile
	var sharedProfile types.SharedProfile
	var sharedProfiles []types.SharedProfile
	var profilesToInstall []types.DeviceProfile
	var profilesToRemove []types.DeviceProfile
	var sharedProfilesToInstall []types.SharedProfile
	var sharedProfilesToRemove []types.SharedProfile
	var devices []types.Device
	var profileLists []types.ProfileList

	if device.UDID == "" {
		err := errors.New("Device UDID cannot be empty")
		return errors.Wrap(err, "VerifyMDMProfiles")
	}
	// Get the profiles that should be installed on the device
	err := db.DB.Where("device_ud_id = ? AND installed = true", device.UDID).Find(&profiles).Error
	if err != nil {
		return errors.Wrap(err, "VerifyMDMProfiles: Cannot load device profiles to install")
	}

	for i := range profileListData.ProfileList {
		incomingProfile := profileListData.ProfileList[i]
		if incomingProfile.PayloadUUID == "" {
			err = errors.New("Profile must have a PayloadUUID")
			ErrorLogger(LogHolder{Message: err.Error()})
		}
		profileLists = append(profileLists, incomingProfile)
	}

	if len(profileLists) == 0 {
		err := errors.New("No Profiles in ProfileList data")
		return errors.Wrap(err, "VerifyMDMProfiles")
	}

	dberr := db.DB.Model(&device).Association("ProfileList").Replace(profileLists)
	if dberr != nil {
		return errors.Wrap(err, "VerifyMDMProfiles: Cannot replace Profile List")
	}

	// For each, loop over the present profiles
	for i := range profiles {
		found := false
		savedProfile := profiles[i]
		for i := range profileListData.ProfileList {
			incomingProfile := profileListData.ProfileList[i]
			if savedProfile.HashedPayloadUUID == incomingProfile.PayloadUUID && savedProfile.PayloadIdentifier == incomingProfile.PayloadIdentifier {
				// If missing, queue up to be installed
				InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: savedProfile.HashedPayloadUUID, ProfileIdentifier: savedProfile.PayloadIdentifier, Message: "Device Profile is installed"})
				found = true
				continue
			}
		}

		if !found {
			InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: savedProfile.HashedPayloadUUID, ProfileIdentifier: savedProfile.PayloadIdentifier, Message: "Device Profile is not installed"})
			profilesToInstall = append(profilesToInstall, savedProfile)
		}
	}

	devices = append(devices, device)
	_, err = PushProfiles(devices, profilesToInstall)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}

	err = db.DB.Model(&sharedProfile).Find(&sharedProfiles).Where("installed = true").Scan(&sharedProfiles).Error
	if err != nil {
		return errors.Wrap(err, "VerifyMDMProfiles: Cannot load shared profiles to install")
	}

	for _, savedSharedProfile := range sharedProfiles {
		found := false
		for i := range profileListData.ProfileList {
			incomingProfile := profileListData.ProfileList[i]
			if savedSharedProfile.HashedPayloadUUID == incomingProfile.PayloadUUID && savedSharedProfile.PayloadIdentifier == incomingProfile.PayloadIdentifier {
				InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: savedSharedProfile.HashedPayloadUUID, ProfileIdentifier: savedSharedProfile.PayloadIdentifier, Message: "Shared Profile is installed"})
				found = true
				continue
			}
		}

		// Make sure we aren't managing this at a device level
		// check this is working!
		// for i := range profilesToInstall {
		// 	deviceProfile := profilesToInstall[i]
		// 	if savedSharedProfile.PayloadIdentifier == deviceProfile.PayloadIdentifier {
		// 		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: savedSharedProfile.HashedPayloadUUID, ProfileIdentifier: savedSharedProfile.PayloadIdentifier, Message: "Shared Profile is a device profile, skipping"})
		// 		found = true
		// 		continue
		// 	}
		// }

		if !found {
			InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: savedSharedProfile.HashedPayloadUUID, ProfileIdentifier: savedSharedProfile.PayloadIdentifier, Message: "Shared Profile is not installed"})
			sharedProfilesToInstall = append(sharedProfilesToInstall, savedSharedProfile)
		}
	}

	_, err = PushSharedProfiles(devices, sharedProfilesToInstall)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}

	// Get the profiles that should be removed from the device
	err = db.DB.Model(&profile).Where("device_ud_id = ? AND installed = false", device.UDID).Scan(&profiles).Error
	if err != nil {
		return errors.Wrap(err, "VerifyMDMProfiles: Cannot load device profiles to remove")
	}

	for i := range profiles {
		savedProfile := profiles[i]
		for i := range profileListData.ProfileList {
			incomingProfile := profileListData.ProfileList[i]
			// DebugLogger(LogHolder{Message: incomingProfile.PayloadIdentifier})
			if savedProfile.PayloadIdentifier == incomingProfile.PayloadIdentifier {
				// If missing, queue up to be installed
				profilesToRemove = append(profilesToRemove, savedProfile)
				DebugLogger(LogHolder{Message: fmt.Sprint(len(profilesToRemove))})
				continue
			}
		}
	}

	DeleteDeviceProfiles(devices, profilesToRemove)

	err = db.DB.Model(&sharedProfile).Find(&sharedProfiles).Where("installed = false").Scan(&sharedProfiles).Error
	if err != nil {
		return errors.Wrap(err, "VerifyMDMProfiles: Cannot load shared profiles to remove")
	}

	for i := range sharedProfiles {
		savedSharedProfile := sharedProfiles[i]
		for i := range profileListData.ProfileList {
			incomingProfile := profileListData.ProfileList[i]
			if savedSharedProfile.PayloadIdentifier == incomingProfile.PayloadIdentifier {
				sharedProfilesToRemove = append(sharedProfilesToRemove, savedSharedProfile)
			}
		}

	}

	DeleteSharedProfiles(devices, sharedProfilesToRemove)

	return nil
}

func GetDeviceProfiles(w http.ResponseWriter, r *http.Request) {
	var profiles []types.DeviceProfile
	vars := mux.Vars(r)

	err := db.DB.Find(&profiles).Where("device_ud_id = ?", vars["udid"]).Scan(&profiles).Error
	if err != nil {
		log.Errorf("Couldn't scan to Device Profiles model: %v", err)
	}
	output, err := json.MarshalIndent(&profiles, "", "    ")
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
}

func GetSharedProfiles(w http.ResponseWriter, r *http.Request) {
	var profiles []types.SharedProfile

	err := db.DB.Find(&profiles).Scan(&profiles).Error
	if err != nil {
		log.Error("Couldn't scan to Shared Profiles model", err)
	}
	output, err := json.MarshalIndent(&profiles, "", "    ")
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
}

// Sign takes an unsigned payload and signs it with the provided private key and certificate.
func SignProfile(key crypto.PrivateKey, cert *x509.Certificate, mobileconfig []byte) ([]byte, error) {
	var err error
	sd, err := pkcs7.NewSignedData(mobileconfig)
	if err != nil {
		return nil, errors.Wrap(err, "create signed data for mobileconfig")
	}

	if err := sd.AddSigner(cert, key, pkcs7.SignerInfoConfig{}); err != nil {
		return nil, errors.Wrap(err, "add crypto signer to mobileconfig signed data")
	}

	signedMobileconfig, err := sd.Finish()
	return signedMobileconfig, errors.Wrap(err, "complete mobileconfig signing")
}

func loadSigningKey(keyPass, keyPath, certPath string) (crypto.PrivateKey, *x509.Certificate, error) {
	var err error
	certData, err := ioutil.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}

	isP12 := filepath.Ext(certPath) == ".p12"
	if isP12 {
		pkey, cert, err := pkcs12.Decode(certData, keyPass)
		return pkey, cert, errors.Wrap(err, "decode p12 contents")
	}

	keyData, err := ioutil.ReadFile(keyPath)
	if err != nil {
		return nil, nil, errors.Wrap(err, "read key from file")
	}

	keyDataBlock, _ := pem.Decode(keyData)
	if keyDataBlock == nil {
		return nil, nil, errors.Errorf("invalid PEM data for private key %s", keyPath)
	}
	var pemKeyData []byte
	if x509.IsEncryptedPEMBlock(keyDataBlock) {
		pemKeyData, err = x509.DecryptPEMBlock(keyDataBlock, []byte(keyPass))
		if err != nil {
			return nil, nil, fmt.Errorf("decrypting DES private key %s", err)
		}
	} else {
		pemKeyData = keyDataBlock.Bytes
	}

	priv, err := x509.ParsePKCS1PrivateKey(pemKeyData)
	if err != nil {
		return nil, nil, errors.Wrap(err, "parse private key")
	}

	pub, _ := pem.Decode(certData)
	if pub == nil {
		return nil, nil, errors.Errorf("invalid PEM data for certificate %q", certPath)
	}

	cert, err := x509.ParseCertificate(pub.Bytes)
	if err != nil {
		return nil, nil, errors.Wrap(err, "parse PEM certificate data")
	}

	return priv, cert, nil
}

func RequestProfileList(device types.Device) error {
	requestType := "ProfileList"
	log.Debugf("Requesting Profile List for %v", device.UDID)
	var commandPayload types.CommandPayload
	commandPayload.UDID = device.UDID
	commandPayload.RequestType = requestType

	_, err := SendCommand(commandPayload)
	if err != nil {
		return errors.Wrap(err, "RequestProfileList: SendCommand")
	}

	return nil
}

func InstallAllProfiles(device types.Device) ([]types.Command, error) {
	var profile types.DeviceProfile
	var profiles []types.DeviceProfile
	var sharedProfile types.SharedProfile
	var sharedProfiles []types.SharedProfile
	var devices []types.Device

	var pushedCommands []types.Command

	devices = append(devices, device)

	// Get the profiles that should be installed on the device
	err := db.DB.Model(&profile).Where("device_ud_id = ? AND installed = true", device.UDID).Scan(&profiles).Error
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
	log.Debugf("Pushing Profiles %v", device.UDID)
	commands, err := PushProfiles(devices, profiles)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	} else {
		pushedCommands = append(pushedCommands, commands...)
	}

	err = db.DB.Model(&sharedProfile).Find(&sharedProfiles).Where("installed = true").Scan(&sharedProfiles).Error
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}

	log.Debugf("Pushing Shared Profiles %v", device.UDID)
	commands, err = PushSharedProfiles(devices, sharedProfiles)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	} else {
		pushedCommands = append(pushedCommands, commands...)
	}

	err = RequestProfileList(device)
	if err != nil {
		return pushedCommands, errors.Wrap(err, "RequestProfileList")
	}

	return pushedCommands, nil
}
