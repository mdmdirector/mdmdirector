package director

import (
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
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
	"github.com/jinzhu/gorm"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func PostProfileHandler(w http.ResponseWriter, r *http.Request) {
	var profiles []types.DeviceProfile
	var sharedProfiles []types.SharedProfile
	var devices []types.Device
	var out types.ProfilePayload
	var metadata []types.MetadataItem

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		log.Error(err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}

	useMetadata := out.Metadata

	for _, payload := range out.Mobileconfigs {
		var profile types.DeviceProfile
		var sharedProfile types.SharedProfile
		mobileconfig, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			log.Error(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		err = plist.Unmarshal(mobileconfig, &profile)
		if err != nil {
			log.Error(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		var tempProfileDict map[string]interface{}
		err = plist.Unmarshal(mobileconfig, &tempProfileDict)
		if err != nil {
			log.Error(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		profile.HashedPayloadUUID = uuid.NewSHA1(uuid.NameSpaceDNS, mobileconfig).String()

		tempProfileDict["PayloadUUID"] = profile.HashedPayloadUUID

		mobileconfig, err = plist.MarshalIndent(&tempProfileDict, "\t")
		if err != nil {
			log.Error(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		profile.MobileconfigData = mobileconfig
		mobileconfigData := mobileconfig
		hash := sha256.Sum256(mobileconfigData)
		profile.MobileconfigHash = hash[:]

		profiles = append(profiles, profile)

		err = plist.Unmarshal(mobileconfig, &sharedProfile)
		if err != nil {
			log.Error(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		sharedProfile.HashedPayloadUUID = uuid.NewSHA1(uuid.NameSpaceDNS, mobileconfig).String()

		var sharedTempProfileDict map[string]interface{}
		err = plist.Unmarshal(mobileconfig, &sharedTempProfileDict)
		if err != nil {
			log.Error(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		sharedTempProfileDict["PayloadUUID"] = sharedProfile.HashedPayloadUUID

		mobileconfig, err = plist.MarshalIndent(&sharedTempProfileDict, "\t")
		if err != nil {
			log.Error(err)
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
					log.Error(err)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				err = SaveSharedProfiles(sharedProfiles)
				if err != nil {
					log.Error(err)
				}

				if out.PushNow {
					_, err = PushSharedProfiles(devices, sharedProfiles)
					if err != nil {
						log.Error(err)
					}
				}
			} else {
				// Individual devices
				for _, item := range out.DeviceUDIDs {
					device, err := GetDevice(item)
					if err != nil {
						log.Error(err)
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}
					metadataItem, err := ProcessDeviceProfiles(device, profiles, out.PushNow, "post")
					if err != nil {
						log.Error(err)
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
					log.Error(err)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				err = SaveSharedProfiles(sharedProfiles)
				if err != nil {
					log.Error(err)
				}

				if out.PushNow {
					_, err = PushSharedProfiles(devices, sharedProfiles)
					if err != nil {
						log.Error(err)
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
						log.Error(err)
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
			log.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
		}

		_, err = w.Write(output)
		if err != nil {
			log.Error(err)
		}
	}
}

func ProcessDeviceProfiles(device types.Device, profiles []types.DeviceProfile, pushNow bool, requestType string) (types.MetadataItem, error) {
	var metadata types.MetadataItem
	var devices []types.Device
	var profileMetadataList []types.ProfileMetadata

	metadata.Device = device
	for i := range profiles {
		var incomingProfiles []types.DeviceProfile
		var profileMetadata types.ProfileMetadata
		status := "unchanged"
		profile := profiles[i]

		incomingProfiles = append(incomingProfiles, profile)
		devices = append(devices, device)
		if requestType == "post" {
			profileDiffers, err := SavedDeviceProfileDiffers(device, profile)
			if err != nil {
				return metadata, errors.Wrap(err, "Could not determine if saved profile differs from incoming profile.")
			}
			if profileDiffers {
				SaveProfiles(devices, incomingProfiles)
				status = "changed"
				if pushNow {
					_, err = PushProfiles(devices, profiles)
					if err != nil {
						log.Error(err)
					}
					status = "pushed"
				}
			}

		} else if requestType == "delete" {
			profilePresent, err := SavedProfileIsPresent(device, profile)
			if err != nil {
				return metadata, errors.Wrap(err, "Could not determine if saved profile is present.")
			}

			if profilePresent {
				DebugLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, ProfileIdentifier: profile.PayloadIdentifier})
				err = db.DB.Model(&profiles).Where("payload_identifier = ? AND device_ud_id = ?", profile.PayloadIdentifier, device.UDID).Update(map[string]interface{}{
					"installed": false,
				}).Error
				if err != nil {
					return metadata, errors.Wrap(err, "Could not set profile to installed = false.")
				}
			}
			if pushNow {
				DeleteDeviceProfiles(devices, incomingProfiles)
			}
		}

		profileMetadata.HashedPayloadUUID = profile.HashedPayloadUUID
		profileMetadata.PayloadIdentifier = profile.PayloadIdentifier
		profileMetadata.PayloadUUID = profile.PayloadUUID
		profileMetadata.Status = status
		profileMetadataList = append(profileMetadataList, profileMetadata)

	}

	metadata.ProfileMetadata = profileMetadataList

	return metadata, nil
}

func SavedProfileIsPresent(device types.Device, profile types.DeviceProfile) (bool, error) {
	var savedProfile types.DeviceProfile
	var profileList types.ProfileList
	// Make sure profile is marked as install = false
	if err := db.DB.Where("device_ud_id = ? AND payload_identifier = ? AND installed = ?", device.UDID, profile.PayloadIdentifier, false).First(&savedProfile).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			return true, nil
		}
	}
	// Make sure the profile isn't in the device's profilelist
	err := db.DB.Model(&profileList).Where("device_ud_id = ? AND payload_identifier = ?", device.UDID, profile.PayloadIdentifier).First(&profileList).Error
	if err != nil {
		if gorm.IsRecordNotFoundError(err) {
			// If it's not found, we'll catch in the false return at the end. Else raise an error
			return true, errors.Wrap(err, "Could not load ProfileList for device")
		}
	}

	return false, nil
}

func SavedDeviceProfileDiffers(device types.Device, profile types.DeviceProfile) (bool, error) {
	var savedProfile types.DeviceProfile
	var profileList types.ProfileList
	// Profile isn't in the db
	if err := db.DB.Where("device_ud_id = ? AND payload_identifier = ? AND installed = ?", device.UDID, profile.PayloadIdentifier, true).First(&savedProfile).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			return true, nil
		}
	}

	// Hash doesn't match
	if savedProfile.HashedPayloadUUID != profile.HashedPayloadUUID {
		log.Debugf("hashes do not match: saved profile %v incoming profile %v", savedProfile.HashedPayloadUUID, profile.HashedPayloadUUID)
		return true, nil
	}

	// Profile isn't what we have saved in the profilelist
	err := db.DB.Model(&profileList).Where("device_ud_id = ? AND payload_identifier = ?", device.UDID, profile.PayloadIdentifier).First(&profileList).Error
	if err != nil {
		if !gorm.IsRecordNotFoundError(err) {
			// If it's not found, we'll catch in the false return at the end. Else raise an error
			return true, errors.Wrap(err, "Could not load ProfileList for device")
		}
	}

	if !strings.EqualFold(profileList.PayloadUUID, profile.HashedPayloadUUID) {
		log.Debugf("hashes do not match: saved profilelist %v incoming profile %v", profileList.PayloadUUID, profile.HashedPayloadUUID)
		return true, nil
	}

	log.Debug("Profile has not changed ", profile.HashedPayloadUUID)
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
		err = db.DB.Model(&sharedProfileModel).Where("payload_identifier = ?", profile.PayloadIdentifier).Update("installed = ?", false).Update("installed", false).Error
		if err != nil {
			return errors.Wrap(err, "Profiles::DisableSharedProfiles: Could not set installed = false")
		}
	}
	DeleteSharedProfiles(devices, sharedProfiles)
	return nil
}

func DeleteProfileHandler(w http.ResponseWriter, r *http.Request) {
	var profiles []types.DeviceProfile
	var profilesModel types.DeviceProfile
	var devices []types.Device
	var out types.DeleteProfilePayload
	var metadata []types.MetadataItem

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		log.Error(err)
	}
	if out.DeviceUDIDs != nil {
		// Not empty list
		if len(out.DeviceUDIDs) > 0 {
			// Targeting all devices
			if out.DeviceUDIDs[0] == "*" {
				err = DisableSharedProfiles(out)
				if err != nil {
					log.Error(err)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				return
			}
			err := db.DB.Model(&devices).Where("ud_id IN (?)", out.DeviceUDIDs).Scan(&devices).Error
			if err != nil {
				log.Error(err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}

		}
	}
	if out.SerialNumbers != nil {
		if len(out.SerialNumbers) > 0 {
			if out.SerialNumbers[0] == "*" {
				err = DisableSharedProfiles(out)
				if err != nil {
					log.Error(err)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				return
			}
			err := db.DB.Model(&devices).Where("serial_number IN (?)", out.SerialNumbers).Scan(&devices).Error
			if err != nil {
				log.Error(err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}

		}
	}

	for i := range devices {
		device, err := FetchDeviceModelAndRelations(devices[i])
		if err != nil {
			log.Error(err)
		}
		for i := range out.Mobileconfigs {
			var profile types.DeviceProfile
			profile.PayloadIdentifier = out.Mobileconfigs[i].PayloadIdentifier
			err = db.DB.Model(&profilesModel).Where("payload_identifier = ?", profile.PayloadIdentifier).Update("installed = ?", false).Update("installed", false).Scan(&profiles).Error
			if err != nil {
				log.Error(err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}

			profiles = append(profiles, profile)
		}

		metadataItem, err := ProcessDeviceProfiles(device, profiles, out.PushNow, "delete")
		if err != nil {
			log.Error(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		metadata = append(metadata, metadataItem)
	}

	if out.Metadata {
		output, err := json.MarshalIndent(&metadata, "", "    ")
		if err != nil {
			log.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
		}

		_, err = w.Write(output)
		if err != nil {
			log.Error(err)
		}
	}

}

func SaveProfiles(devices []types.Device, profiles []types.DeviceProfile) {
	var profile types.DeviceProfile
	for i := range devices {
		device := devices[i]
		for i := range profiles {
			profileData := profiles[i]
			if profileData.PayloadIdentifier != "" {
				db.DB.Model(&profile).Where("device_ud_id = ?", device.UDID).Where("payload_identifier = ?", profileData.PayloadIdentifier).Delete(&profile)
			}
		}
		db.DB.Model(&device).Association("Profiles").Append(profiles)
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

			InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Pushing Shared Profile", ProfileIdentifier: profileData.PayloadIdentifier, ProfileUUID: profileData.HashedPayloadUUID, CommandRequestType: commandPayload.RequestType})

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
				log.Error(err)
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
				log.Error(err)
				return errors.Wrap(err, "Deleting shared profiles")
			}
		}
	}

	tx2 := db.DB.Model(&profile)
	for _, profileData := range profiles {
		// utils.PrintStruct(profileData)
		err := tx2.Create(&profileData).Error
		if err != nil {
			log.Error(err)
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
			_, err := SendCommand(commandPayload)
			if err != nil {
				log.Error(err)
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
			_, err := SendCommand(commandPayload)
			if err != nil {
				log.Error(err)
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

	// Get the profiles that should be installed on the device
	err := db.DB.Model(&profile).Where("device_ud_id = ? AND installed = true", device.UDID).Scan(&profiles).Error
	if err != nil {
		return errors.Wrap(err, "VerifyMDMProfiles: Cannot load device profiles to install")
	}

	for i := range profileListData.ProfileList {
		incomingProfile := profileListData.ProfileList[i]
		profileLists = append(profileLists, incomingProfile)
	}

	err = db.DB.Model(&device).Association("ProfileList").Replace(profileLists).Error
	if err != nil {
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
				found = true
				continue
			}
		}

		if !found {
			profilesToInstall = append(profilesToInstall, savedProfile)
		}
	}

	devices = append(devices, device)
	_, err = PushProfiles(devices, profilesToInstall)
	if err != nil {
		log.Error(err)
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
				found = true
				continue
			}
		}

		// Make sure we aren't managing this at a device level
		for i := range profilesToInstall {
			deviceProfile := profilesToInstall[i]
			if savedSharedProfile.HashedPayloadUUID == deviceProfile.PayloadUUID && savedSharedProfile.PayloadIdentifier == deviceProfile.PayloadIdentifier {
				found = true
				continue
			}
		}

		if !found {
			sharedProfilesToInstall = append(sharedProfilesToInstall, savedSharedProfile)
		}
	}

	_, err = PushSharedProfiles(devices, sharedProfilesToInstall)
	if err != nil {
		log.Error(err)
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
			if savedProfile.PayloadIdentifier == incomingProfile.PayloadIdentifier {
				// If missing, queue up to be installed
				profilesToRemove = append(profilesToRemove, savedProfile)
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
				found := false
				// Make sure the profile isn't being managed at a device level
				for i := range profilesToInstall {
					deviceProfile := profilesToInstall[i]
					if savedSharedProfile.PayloadIdentifier == deviceProfile.PayloadIdentifier {
						found = true
					}
				}
				if !found {
					sharedProfilesToRemove = append(sharedProfilesToRemove, savedSharedProfile)
				}

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
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		log.Error(err)
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
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		log.Error(err)
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

func RequestProfileList(device types.Device) {
	requestType := "ProfileList"
	log.Debugf("Requesting Profile List for %v", device.UDID)
	var commandPayload types.CommandPayload
	commandPayload.UDID = device.UDID
	commandPayload.RequestType = requestType

	_, err := SendCommand(commandPayload)
	if err != nil {
		log.Error(err)
	}
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
		log.Error(err)
	}
	log.Debugf("Pushing Profiles %v", device.UDID)
	commands, err := PushProfiles(devices, profiles)
	if err != nil {
		log.Error(err)
	} else {
		pushedCommands = append(pushedCommands, commands...)
	}

	err = db.DB.Model(&sharedProfile).Find(&sharedProfiles).Where("installed = true").Scan(&sharedProfiles).Error
	if err != nil {
		log.Error(err)
	}

	log.Debugf("Pushing Shared Profiles %v", device.UDID)
	commands, err = PushSharedProfiles(devices, sharedProfiles)
	if err != nil {
		log.Error(err)
	} else {
		pushedCommands = append(pushedCommands, commands...)
	}

	RequestProfileList(device)

	return pushedCommands, nil
}
