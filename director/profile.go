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
	var (
		profiles       []types.DeviceProfile
		sharedProfiles []types.SharedProfile
		devices        []types.Device
		out            types.ProfilePayload
		metadata       []types.MetadataItem
	)

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

	// If targeting devices by UDID
	if out.DeviceUDIDs != nil && len(out.DeviceUDIDs) > 0 {
		// If targeting all devices
		if out.DeviceUDIDs[0] == "*" {
			// Get all devices and save shared profiles
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

			// If push now flag is set, push shared profiles to devices
			if out.PushNow {
				_, err = PushSharedProfiles(devices, sharedProfiles)
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}
			}
		} else { // If targeting individual devices by UDID
			for _, item := range out.DeviceUDIDs {
				// Get device and process profiles for device
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
	} else if out.SerialNumbers != nil && len(out.SerialNumbers) > 0 { // If targeting devices by serial number
		// If targeting all devices
		if out.SerialNumbers[0] == "*" {
			// Get all devices and save shared profiles
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

			// If push now flag is set, push shared profiles to devices
			if out.PushNow {
				_, err = PushSharedProfiles(devices, sharedProfiles)
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}
			}
		} else { // If targeting individual devices by serial number
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
	var (
		metadata            types.MetadataItem
		devices             []types.Device
		profileMetadataList []types.ProfileMetadata
		profilesToSave      []types.DeviceProfile
	)
	// metadata.Device = device
	for i := range profiles {
		status := "unchanged"
		profile := profiles[i]

		devices = append(devices, device)
		switch requestType {
		case "post":
			profileDiffers, err := SavedDeviceProfileDiffers(device, profile)
			if err != nil {
				return metadata, errors.Wrap(err, "Could not determine if saved profile differs from incoming profile.")
			}
			profile.Installed = true
			if profileDiffers {
				profilesToSave = append(profilesToSave, profile)
				if pushNow {
					_, err = PushProfiles(devices, []types.DeviceProfile{profile})
					if err != nil {
						ErrorLogger(LogHolder{Message: err.Error()})
						break
					}
					status = "pushed"
				} else {
					status = "saved"
				}
				status = "changed"
			}

			// Cleanup the old duplicated profiles
			// Remove this in a later version when this is not a problem anymore
			err = db.DB.Model(&types.DeviceProfile{}).Where("device_ud_id = ? AND payload_identifier = ?", device.UDID, profile.PayloadIdentifier).Not("hashed_payload_uuid = ?", profile.HashedPayloadUUID).Delete(&types.DeviceProfile{}).Error
			if err != nil {
				if !intErrors.Is(err, gorm.ErrRecordNotFound) {
					return metadata, errors.Wrap(err, "Cleanup old profiles")
				}
			}

		case "delete":
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

		default:
			return metadata, errors.Errorf("unsupported request type: %s", requestType)
		}

		profileMetadataList = append(profileMetadataList, types.ProfileMetadata{
			HashedPayloadUUID: profile.HashedPayloadUUID,
			PayloadIdentifier: profile.PayloadIdentifier,
			PayloadUUID:       profile.PayloadUUID,
			Status:            status,
		})

	}
	// Save updated profiles to the device database
	err := SaveProfiles(devices, profilesToSave)
	if err != nil {
		return metadata, errors.Wrap(err, "SaveProfiles")
	}

	// Build profile metadata for response
	metadata.ProfileMetadata = profileMetadataList

	return metadata, nil

}

func SavedProfileIsPresent(device types.Device, profile types.DeviceProfile) (bool, error) {
	var savedProfile types.DeviceProfile
	// Make sure profile is marked as install = false
	if err := db.DB.Where("device_ud_id = ? AND payload_identifier = ? AND installed = ?", device.UDID, profile.PayloadIdentifier, false).First(&savedProfile).Error; err != nil {
		if !intErrors.Is(err, gorm.ErrRecordNotFound) {
			return false, errors.Wrap(err, "SavedProfileIsPresent: Cannot retrieve saved profile")
		}
		// Record not found, the profile is not present
		return false, nil
	}

	// The profile is present if it's installed
	if savedProfile.Installed {
		DebugLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, ProfileIdentifier: profile.PayloadIdentifier, Message: "Profile present and marked as installed = true"})
		return true, nil
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
	var (
		profiles []types.DeviceProfile
		devices  []types.Device
		out      types.DeleteProfilePayload
		metadata []types.MetadataItem
	)
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
	pushedCommands := make([]types.Command, 0, len(devices)*len(profiles))

	for _, device := range devices {
		for _, profileData := range profiles {
			commandPayload := types.CommandPayload{
				UDID:        device.UDID,
				RequestType: "RemoveProfile",
				Identifier:  profileData.PayloadIdentifier,
			}
			InfoLogger(LogHolder{
				DeviceUDID:         device.UDID,
				DeviceSerial:       device.SerialNumber,
				Message:            "Deleting Shared Profile",
				ProfileIdentifier:  profileData.PayloadIdentifier,
				ProfileUUID:        profileData.HashedPayloadUUID,
				CommandRequestType: commandPayload.RequestType,
			})

			command, err := SendCommand(commandPayload)
			if err != nil {
				return pushedCommands, errors.Wrap(err, "failed to send RemoveProfile command")
			}
			pushedCommands = append(pushedCommands, command)
		}
	}

	return pushedCommands, nil
}

func DeleteDeviceProfiles(devices []types.Device, profiles []types.DeviceProfile) ([]types.Command, error) {
	pushedCommands := make([]types.Command, 0, len(devices)*len(profiles))

	for _, device := range devices {
		for _, profileData := range profiles {
			commandPayload := types.CommandPayload{
				UDID:        device.UDID,
				RequestType: "RemoveProfile",
				Identifier:  profileData.PayloadIdentifier,
			}
			InfoLogger(LogHolder{
				DeviceUDID:         device.UDID,
				DeviceSerial:       device.SerialNumber,
				Message:            "Deleting Device Profile",
				ProfileIdentifier:  profileData.PayloadIdentifier,
				ProfileUUID:        profileData.HashedPayloadUUID,
				CommandRequestType: commandPayload.RequestType,
			})

			command, err := SendCommand(commandPayload)
			if err != nil {
				return pushedCommands, errors.Wrap(err, "failed to send RemoveProfile command")
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
	InfoLogger(LogHolder{
		DeviceUDID:   device.UDID,
		DeviceSerial: device.SerialNumber,
		Message:      "Verifying MDM Profiles",
	},
	)

	var (
		profiles                []types.DeviceProfile
		sharedProfile           types.SharedProfile
		sharedProfiles          []types.SharedProfile
		profilesToInstall       []types.DeviceProfile
		profilesToRemove        []types.DeviceProfile
		sharedProfilesToInstall []types.SharedProfile
		sharedProfilesToRemove  []types.SharedProfile
		devices                 []types.Device
		profileLists            []types.ProfileList
		profilesForVerification []ProfileForVerification
	)

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
		deviceProfile := profiles[i]
		profileForVerification := ProfileForVerification{
			Type:              "device",
			PayloadUUID:       deviceProfile.PayloadUUID,
			PayloadIdentifier: deviceProfile.PayloadIdentifier,
			HashedPayloadUUID: deviceProfile.HashedPayloadUUID,
			MobileconfigData:  deviceProfile.MobileconfigData,
			MobileconfigHash:  deviceProfile.MobileconfigHash,
			DeviceUDID:        deviceProfile.DeviceUDID,
			Installed:         deviceProfile.Installed,
		}
		profilesForVerification = append(profilesForVerification, profileForVerification)
	}

	err = db.DB.Model(&sharedProfile).Find(&sharedProfiles).Scan(&sharedProfiles).Error
	if err != nil {
		return errors.Wrap(err, "VerifyMDMProfiles: Cannot load shared profiles to install")
	}

	for i := range sharedProfiles {
		sharedProfile := sharedProfiles[i]
		profileForVerification := ProfileForVerification{
			Type:              "shared",
			PayloadUUID:       sharedProfile.PayloadUUID,
			PayloadIdentifier: sharedProfile.PayloadIdentifier,
			HashedPayloadUUID: sharedProfile.HashedPayloadUUID,
			MobileconfigData:  sharedProfile.MobileconfigData,
			MobileconfigHash:  sharedProfile.MobileconfigHash,
			Installed:         sharedProfile.Installed,
		}
		profilesForVerification = append(profilesForVerification, profileForVerification)
	}

	_, cert, err := loadSigningKey(utils.KeyPassword(), utils.KeyPath(), utils.CertPath())
	if err != nil {
		log.Errorf("loading signing certificate and private key: %v", err)
	}

	// ensure certificate matches on enrollment profile
	err = ensureCertOnEnrollmentProfile(device, profileLists, cert)
	if err != nil {
		return errors.Wrap(err, "checkCertOnEnrollmentProfile")
	}

	for i := range profilesForVerification {
		profileForVerification := profilesForVerification[i]
		isInstalled, needsReinstall, err := validateProfileInProfileList(profileForVerification, profileLists, cert)
		if err != nil {
			return errors.Wrap(err, "validateProfileInProfileList")
		}

		switch {
		// Profile is present in the ProfileList output
		case isInstalled:
			switch {
			// Profile is present, but should not be installed
			case !profileForVerification.Installed:
				InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: profileForVerification.HashedPayloadUUID, ProfileIdentifier: profileForVerification.PayloadIdentifier, Message: "VerifyMDMProfiles: Profile is present but should not be installed", Metric: profileForVerification.Type})
				sharedProfilesToRemove, profilesToRemove = addProfileToLists(profileForVerification, sharedProfilesToRemove, profilesToRemove)

			// Profile is present, but the hash doesn't match
			case needsReinstall:
				InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: profileForVerification.HashedPayloadUUID, ProfileIdentifier: profileForVerification.PayloadIdentifier, Message: "VerifyMDMProfiles: Profile is present but profile needs to be reinstalled", Metric: profileForVerification.Type})
				sharedProfilesToInstall, profilesToInstall = addProfileToLists(profileForVerification, sharedProfilesToInstall, profilesToInstall)

			// Profile is present and does not need to be reinstalled, success
			default:
				InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: profileForVerification.HashedPayloadUUID, ProfileIdentifier: profileForVerification.PayloadIdentifier, Message: "VerifyMDMProfiles: Profile is present and profile does not require reinstallation", Metric: profileForVerification.Type})
			}

		// Profile is not in the profileList
		default:
			switch {
			// But it should be installed
			case profileForVerification.Installed:
				InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileUUID: profileForVerification.HashedPayloadUUID, ProfileIdentifier: profileForVerification.PayloadIdentifier, Message: "VerifyMDMProfiles: Profile is present not in the profile list and should be installed", Metric: profileForVerification.Type})
				sharedProfilesToInstall, profilesToInstall = addProfileToLists(profileForVerification, sharedProfilesToInstall, profilesToInstall)

			// Not present, and shouldn't be installed
			default:
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

// Checks if a) if the certificate used to sign the profile returned by ProfileList matches the one we have locally (if we are signing profiles), b) if the certificate returned by ProfileList has the same hashed payload as the one we have locally and c) if the profile is present, but we should remove it
// Returned bool #1 represents whether the profile is currently installed installed
// Returned bool #2 represents whether the profile needs to be reinstalled

func validateProfileInProfileList(profileForVerification ProfileForVerification, profileLists []types.ProfileList, signingCert *x509.Certificate) (bool, bool, error) {
	for i := range profileLists {
		profileList := profileLists[i]

		// Verify the certifacte
		if utils.Sign() && profileForVerification.PayloadIdentifier == profileList.PayloadIdentifier {
			InfoLogger(LogHolder{ProfileIdentifier: profileForVerification.PayloadIdentifier, Message: "Verifying signing certificate for profile"})
			certMatched := false
			for _, cert := range profileList.SignerCertificates {
				parsed, err := x509.ParseCertificate(cert)
				if err != nil {
					return true, false, errors.Wrap(err, "parse PEM certificate data")
				}
				if parsed.Subject.String() == signingCert.Subject.String() && parsed.NotAfter == signingCert.NotAfter && parsed.Issuer.CommonName == signingCert.Issuer.CommonName {
					msg := fmt.Sprintf("%v Parsed certificate matches local signing certificate", signingCert.Subject.String())
					InfoLogger(LogHolder{Message: msg, DeviceUDID: profileList.DeviceUDID})
					certMatched = true
					break
				}
			}
			if !certMatched {
				msg := fmt.Sprintf("%v No certificates found matching local certificates", signingCert.Subject.String())
				InfoLogger(LogHolder{Message: msg, DeviceUDID: profileList.DeviceUDID})
				return true, true, nil
			}
		}
		// Profile is present in profile list, payloaduuid matches what we expect, should be installed
		if profileForVerification.HashedPayloadUUID == profileList.PayloadUUID && profileForVerification.PayloadIdentifier == profileList.PayloadIdentifier {
			return true, false, nil
		}
	}

	// If we get here, we have not found the profile in the ProfileList response
	return false, true, nil
}

// Add to the appropriate list
func addProfileToLists(profileForVerification ProfileForVerification, sharedProfilesToRemove []types.SharedProfile, profilesToRemove []types.DeviceProfile) ([]types.SharedProfile, []types.DeviceProfile) {
	switch profileForVerification.Type {
	case "shared":
		sharedProfile := types.SharedProfile{
			HashedPayloadUUID: profileForVerification.HashedPayloadUUID,
			PayloadUUID:       profileForVerification.PayloadUUID,
			PayloadIdentifier: profileForVerification.PayloadIdentifier,
			Installed:         profileForVerification.Installed,
			MobileconfigData:  profileForVerification.MobileconfigData,
			MobileconfigHash:  profileForVerification.MobileconfigHash,
		}
		sharedProfilesToRemove = append(sharedProfilesToRemove, sharedProfile)

	case "device":
		deviceProfile := types.DeviceProfile{
			HashedPayloadUUID: profileForVerification.HashedPayloadUUID,
			PayloadUUID:       profileForVerification.PayloadUUID,
			PayloadIdentifier: profileForVerification.PayloadIdentifier,
			Installed:         profileForVerification.Installed,
			MobileconfigData:  profileForVerification.MobileconfigData,
			MobileconfigHash:  profileForVerification.MobileconfigHash,
			DeviceUDID:        profileForVerification.DeviceUDID,
		}
		profilesToRemove = append(profilesToRemove, deviceProfile)
	}

	return sharedProfilesToRemove, profilesToRemove
}

func GetDeviceProfiles(w http.ResponseWriter, r *http.Request) {
	var profiles []types.DeviceProfile
	vars := mux.Vars(r)

	err := db.DB.Where("device_ud_id = ?", vars["udid"]).Limit(1).Find(&profiles).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "Device profile not found")
		} else {
			log.Errorf("Error retrieving device profiles: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "Internal server error")
		}
		return
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

	err := db.DB.Find(&profiles).Error
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

	commandPayload := types.CommandPayload{
		UDID:        device.UDID,
		RequestType: requestType,
	}

	_, err := SendCommand(commandPayload)
	if err != nil {
		return errors.Wrap(err, "RequestProfileList: SendCommand")
	}

	return nil
}

func InstallAllProfiles(device types.Device) ([]types.Command, error) {
	var (
		profile        types.DeviceProfile
		profiles       []types.DeviceProfile
		sharedProfile  types.SharedProfile
		sharedProfiles []types.SharedProfile
		pushedCommands []types.Command
	)

	// Retrieve the profiles and shared profiles that should be installed on the device
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&profile).Where("device_ud_id = ? AND installed = true", device.UDID).Find(&profiles).Error; err != nil {
			return errors.Wrap(err, "Failed to retrieve device profiles")
		}

		if err := tx.Model(&sharedProfile).Where("installed = true").Find(&sharedProfiles).Error; err != nil {
			return errors.Wrap(err, "Failed to retrieve shared profiles")
		}

		return nil
	})
	if err != nil {
		return pushedCommands, err
	}

	log.Debugf("Pushing profiles for device %v", device.UDID)

	// Push the device and shared profiles
	commands, err := PushProfiles([]types.Device{device}, profiles)
	if err != nil {
		return pushedCommands, errors.Wrap(err, "Failed to push device profiles")
	}

	pushedCommands = append(pushedCommands, commands...)

	log.Debugf("Pushing shared profiles for device %v", device.UDID)

	commands, err = PushSharedProfiles([]types.Device{device}, sharedProfiles)
	if err != nil {
		return pushedCommands, errors.Wrap(err, "Failed to push shared profiles")
	}

	pushedCommands = append(pushedCommands, commands...)

	// Request the profile list
	if err := RequestProfileList(device); err != nil {
		return pushedCommands, errors.Wrap(err, "Failed to request profile list")
	}

	return pushedCommands, nil
}
