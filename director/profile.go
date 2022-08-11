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
			return
		}
		err = plist.Unmarshal(mobileconfig, &profile)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		var tempProfileDict map[string]interface{}
		err = plist.Unmarshal(mobileconfig, &tempProfileDict)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		profile.HashedPayloadUUID = uuid.NewSHA1(uuid.NameSpaceDNS, mobileconfig).String()

		tempProfileDict["PayloadUUID"] = profile.HashedPayloadUUID

		mobileconfig, err = plist.MarshalIndent(&tempProfileDict, "\t")
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
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
			return
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
			return
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
						return
					}
					InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Processing POST to /profiles"})
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
					return
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
					InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Processing POST to /profiles"})
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
			return
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
					_, err = PushProfiles(devices, []types.DeviceProfile{profile})
					if err != nil {
						ErrorLogger(LogHolder{Message: err.Error()})
					}
					status = "pushed"
				} else {
					status = "saved"
				}
			}

			// Cleanup the old duplicated profiles
			// Remove this in a later version when this is not a problem anymore
			err = db.DB.Model(&types.DeviceProfile{}).Where("device_ud_id = ? AND payload_identifier = ?", device.UDID, profile.PayloadIdentifier).Not("hashed_payload_uuid = ?", profile.HashedPayloadUUID).Delete(&types.DeviceProfile{}).Error
			if err != nil {
				if !intErrors.Is(err, gorm.ErrRecordNotFound) {
					return metadata, errors.Wrap(err, "Cleanup old profiles")
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
				deletedProfile := []types.DeviceProfile{profile}
				_, err := DeleteDeviceProfiles(devices, deletedProfile)
				if err != nil {
					return metadata, errors.Wrap(err, "Delete device profiles")
				}
			}

		}

		profileMetadata.HashedPayloadUUID = profile.HashedPayloadUUID
		profileMetadata.PayloadIdentifier = profile.PayloadIdentifier
		profileMetadata.PayloadUUID = profile.PayloadUUID
		profileMetadata.Status = status
		profileMetadataList = append(profileMetadataList, profileMetadata)

	}

	err := SaveProfiles(devices, profilesToSave)
	if err != nil {
		return metadata, errors.Wrap(err, "SaveProfiles")
	}

	metadata.ProfileMetadata = profileMetadataList

	return metadata, nil
}

func SavedProfileIsPresent(device types.Device, profile types.DeviceProfile) (bool, error) {
	var savedProfile types.DeviceProfile
	// Make sure profile is marked as install = false
	if err := db.DB.Where("device_ud_id = ? AND payload_identifier = ? AND installed = ?", device.UDID, profile.PayloadIdentifier, false).First(&savedProfile).Error; err != nil {
		if intErrors.Is(err, gorm.ErrRecordNotFound) {
			DebugLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, ProfileIdentifier: profile.PayloadIdentifier, Message: "Profile present and marked as installed = true"})
			return true, nil
		}
	}

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
	_, err = DeleteSharedProfiles(devices, sharedProfiles)
	if err != nil {
		return errors.Wrap(err, "Profiles::DisableSharedProfiles: DeleteSharedProfiles")
	}
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
		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Processing DELETE to /profiles"})
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

func SaveProfiles(devices []types.Device, profiles []types.DeviceProfile) error {
	for i := range devices {
		device := devices[i]
		if device.UDID == "" {
			continue
		}

		for profilei := range profiles {
			var boolModel types.DeviceProfile
			profileData := profiles[profilei]
			profileData.DeviceUDID = device.UDID

			err := db.DB.Save(&profileData).Error
			if err != nil {
				if !intErrors.Is(err, gorm.ErrRecordNotFound) {
					return errors.Wrap(err, "Save incoming profile")
				}
			}

			DebugLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, ProfileIdentifier: profileData.PayloadIdentifier, Message: "Updating profile installed bool"})
			err = db.DB.Model(&boolModel).Where("device_ud_id = ? AND payload_identifier = ?", device.UDID, profileData.PayloadIdentifier).Updates(map[string]interface{}{
				"installed": profiles[profilei].Installed,
			}).Error
			if err != nil {
				return errors.Wrap(err, "Update boolean on profile")
			}

		}
	}
	return nil
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

func DeleteSharedProfiles(devices []types.Device, profiles []types.SharedProfile) ([]types.Command, error) {
	var pushedCommands []types.Command
	for i := range devices {
		device := devices[i]
		for i := range profiles {
			profileData := profiles[i]
			var commandPayload types.CommandPayload
			commandPayload.UDID = device.UDID
			commandPayload.RequestType = "RemoveProfile"
			commandPayload.Identifier = profileData.PayloadIdentifier
			InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Deleting Shared Profile", ProfileIdentifier: profileData.PayloadIdentifier, ProfileUUID: profileData.HashedPayloadUUID, CommandRequestType: commandPayload.RequestType})
			command, err := SendCommand(commandPayload)
			if err != nil {
				return pushedCommands, errors.Wrap(err, "DeleteSharedProfiles")
			}
			pushedCommands = append(pushedCommands, command)
		}
	}

	return pushedCommands, nil
}

func DeleteDeviceProfiles(devices []types.Device, profiles []types.DeviceProfile) ([]types.Command, error) {
	var pushedCommands []types.Command
	for i := range devices {
		device := devices[i]
		for i := range profiles {
			profileData := profiles[i]
			var commandPayload types.CommandPayload
			commandPayload.UDID = device.UDID
			commandPayload.RequestType = "RemoveProfile"
			commandPayload.Identifier = profileData.PayloadIdentifier
			InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Deleting Device Profile", ProfileIdentifier: profileData.PayloadIdentifier, ProfileUUID: profileData.HashedPayloadUUID, CommandRequestType: commandPayload.RequestType})
			command, err := SendCommand(commandPayload)
			if err != nil {
				return pushedCommands, errors.Wrap(err, "DeleteDeviceProfiles")
			}
			pushedCommands = append(pushedCommands, command)
		}
	}

	return pushedCommands, nil
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

type ProfileForVerification struct {
	PayloadUUID       string
	PayloadIdentifier string
	HashedPayloadUUID string
	MobileconfigData  []byte
	MobileconfigHash  []byte
	DeviceUDID        string
	Installed         bool
	Type              string // device or shared
}

func VerifyMDMProfiles(profileListData types.ProfileListData, device types.Device) error {
	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Verifying MDM Profiles"})
	var profiles []types.DeviceProfile
	var sharedProfile types.SharedProfile
	var sharedProfiles []types.SharedProfile
	var profilesToInstall []types.DeviceProfile
	var profilesToRemove []types.DeviceProfile
	var sharedProfilesToInstall []types.SharedProfile
	var sharedProfilesToRemove []types.SharedProfile
	var devices []types.Device
	var profileLists []types.ProfileList

	var profilesForVerification []ProfileForVerification

	if device.UDID == "" {
		err := errors.New("Device UDID cannot be empty")
		return errors.Wrap(err, "VerifyMDMProfiles")
	}

	for i := range profileListData.ProfileList {
		incomingProfile := profileListData.ProfileList[i]
		if incomingProfile.PayloadUUID == "" {
			err := errors.New("Profile must have a PayloadUUID")
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
		return errors.Wrap(dberr, "VerifyMDMProfiles: Cannot replace Profile List")
	}

	// Get the device profiles that should be installed on the device
	err := db.DB.Where("device_ud_id = ?", device.UDID).Find(&profiles).Error
	if err != nil {
		return errors.Wrap(err, "VerifyMDMProfiles: Cannot load device profiles to install")
	}

	for i := range profiles {
		var profileForVerification ProfileForVerification
		deviceprofile := profiles[i]
		profileForVerification.PayloadUUID = deviceprofile.PayloadUUID
		profileForVerification.PayloadIdentifier = deviceprofile.PayloadIdentifier
		profileForVerification.HashedPayloadUUID = deviceprofile.HashedPayloadUUID
		profileForVerification.MobileconfigData = deviceprofile.MobileconfigData
		profileForVerification.MobileconfigHash = deviceprofile.MobileconfigHash
		profileForVerification.DeviceUDID = deviceprofile.DeviceUDID
		profileForVerification.Installed = deviceprofile.Installed
		profileForVerification.Type = "device"
		profilesForVerification = append(profilesForVerification, profileForVerification)
	}

	err = db.DB.Model(&sharedProfile).Find(&sharedProfiles).Scan(&sharedProfiles).Error
	if err != nil {
		return errors.Wrap(err, "VerifyMDMProfiles: Cannot load shared profiles to install")
	}

	for i := range sharedProfiles {
		var profileForVerification ProfileForVerification
		sharedProfile := sharedProfiles[i]
		profileForVerification.PayloadUUID = sharedProfile.PayloadUUID
		profileForVerification.PayloadIdentifier = sharedProfile.PayloadIdentifier
		profileForVerification.HashedPayloadUUID = sharedProfile.HashedPayloadUUID
		profileForVerification.MobileconfigData = sharedProfile.MobileconfigData
		profileForVerification.MobileconfigHash = sharedProfile.MobileconfigHash
		profileForVerification.Installed = sharedProfile.Installed
		profileForVerification.Type = "shared"
		profilesForVerification = append(profilesForVerification, profileForVerification)
	}

	for i := range profilesForVerification {
		profileForVerification := profilesForVerification[i]
		isInList, hashMatches := profileInProfileList(profileForVerification, profileLists)
		// Profile is present in the ProfileList output
		if isInList {
			// Profile is present, but should not be installed
			if !profileForVerification.Installed {
				InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: profileForVerification.HashedPayloadUUID, ProfileIdentifier: profileForVerification.PayloadIdentifier, Message: "VerifyMDMProfiles: Profile is present but should not be installed", Metric: profileForVerification.Type})
				sharedProfilesToRemove, profilesToRemove = addProfileToLists(profileForVerification, sharedProfilesToRemove, profilesToRemove)
			}

			// Profile is present, but the hash doesn't match
			if profileForVerification.Installed && !hashMatches {
				InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: profileForVerification.HashedPayloadUUID, ProfileIdentifier: profileForVerification.PayloadIdentifier, Message: "VerifyMDMProfiles: Profile is present but hashed payload UUID does not match", Metric: profileForVerification.Type})
				sharedProfilesToInstall, profilesToInstall = addProfileToLists(profileForVerification, sharedProfilesToInstall, profilesToInstall)
			}

			// Profile is present and hash matches, success
			if profileForVerification.Installed && hashMatches {
				InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: profileForVerification.HashedPayloadUUID, ProfileIdentifier: profileForVerification.PayloadIdentifier, Message: "VerifyMDMProfiles: Profile is present and hashed payload UUID matches", Metric: profileForVerification.Type})
			}
		} else { // Profile is not in the profileList
			// But it should be installed
			if profileForVerification.Installed {
				InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: profileForVerification.HashedPayloadUUID, ProfileIdentifier: profileForVerification.PayloadIdentifier, Message: "VerifyMDMProfiles: Profile is present not in the profile list and should be installed", Metric: profileForVerification.Type})
				sharedProfilesToInstall, profilesToInstall = addProfileToLists(profileForVerification, sharedProfilesToInstall, profilesToInstall)
			} else { // Not present, and shouldn't be installed
				InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: profileForVerification.HashedPayloadUUID, ProfileIdentifier: profileForVerification.PayloadIdentifier, Message: "VerifyMDMProfiles: Profile is not present and should not be installed", Metric: profileForVerification.Type})
			}
		}
	}

	devices = append(devices, device)
	_, err = PushProfiles(devices, profilesToInstall)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}

	_, err = PushSharedProfiles(devices, sharedProfilesToInstall)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}

	_, err = DeleteDeviceProfiles(devices, profilesToRemove)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}

	_, err = DeleteSharedProfiles(devices, sharedProfilesToRemove)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}

	return nil
}

// Checks if a) the profile is in the profile list (bool #1) and b) if the profile should be installed, if the hashes match (bool #2)
func profileInProfileList(profileForVerification ProfileForVerification, profileLists []types.ProfileList) (bool, bool) {
	for i := range profileLists {
		profileList := profileLists[i]
		// Profile is present in profile list, payloaduuid matches what we expect, should be installed
		if profileForVerification.HashedPayloadUUID == profileList.PayloadUUID && profileForVerification.PayloadIdentifier == profileList.PayloadIdentifier && profileForVerification.Installed {
			return true, true
		}

		// Profile is present in profile list, profile should not be installed
		if profileForVerification.PayloadIdentifier == profileList.PayloadIdentifier && !profileForVerification.Installed {
			return true, false
		}
	}
	return false, false
}

// Add to the appropriate list
func addProfileToLists(profileForVerification ProfileForVerification, sharedProfilesToRemove []types.SharedProfile, profilesToRemove []types.DeviceProfile) ([]types.SharedProfile, []types.DeviceProfile) {
	if profileForVerification.Type == "shared" {
		var sharedProfile types.SharedProfile
		sharedProfile.HashedPayloadUUID = profileForVerification.HashedPayloadUUID
		sharedProfile.PayloadUUID = profileForVerification.PayloadUUID
		sharedProfile.PayloadIdentifier = profileForVerification.PayloadIdentifier
		sharedProfile.Installed = profileForVerification.Installed
		sharedProfile.MobileconfigData = profileForVerification.MobileconfigData
		sharedProfile.MobileconfigHash = profileForVerification.MobileconfigHash
		sharedProfilesToRemove = append(sharedProfilesToRemove, sharedProfile)
	}

	if profileForVerification.Type == "device" {
		var deviceProfile types.DeviceProfile
		deviceProfile.HashedPayloadUUID = profileForVerification.HashedPayloadUUID
		deviceProfile.PayloadUUID = profileForVerification.PayloadUUID
		deviceProfile.PayloadIdentifier = profileForVerification.PayloadIdentifier
		deviceProfile.Installed = profileForVerification.Installed
		deviceProfile.MobileconfigData = profileForVerification.MobileconfigData
		deviceProfile.MobileconfigHash = profileForVerification.MobileconfigHash
		deviceProfile.DeviceUDID = profileForVerification.DeviceUDID
		profilesToRemove = append(profilesToRemove, deviceProfile)
	}

	return sharedProfilesToRemove, profilesToRemove
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
