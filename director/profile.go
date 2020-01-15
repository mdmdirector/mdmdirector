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

	"golang.org/x/crypto/pkcs12"

	"github.com/fullsailor/pkcs7"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/groob/plist"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/log"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
)

func PostProfileHandler(w http.ResponseWriter, r *http.Request) {
	var profiles []types.DeviceProfile
	var sharedProfiles []types.SharedProfile
	var devices []types.Device
	var out types.ProfilePayload
	var err error

	err = json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		log.Error(err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}

	for _, payload := range out.Mobileconfigs {
		var profile types.DeviceProfile
		var sharedProfile types.SharedProfile
		mobileconfig, err := base64.StdEncoding.DecodeString(string(payload))
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

		profile.HashedPayloadUUID = uuid.NewSHA1(uuid.NameSpaceDNS, []byte(mobileconfig)).String()

		tempProfileDict["PayloadUUID"] = profile.HashedPayloadUUID

		mobileconfig, err = plist.MarshalIndent(&tempProfileDict, "\t")
		if err != nil {
			log.Error(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		profile.MobileconfigData = mobileconfig
		mobileconfigData := []byte(mobileconfig)
		hash := sha256.Sum256(mobileconfigData)
		profile.MobileconfigHash = hash[:]

		profiles = append(profiles, profile)

		err = plist.Unmarshal(mobileconfig, &sharedProfile)
		if err != nil {
			log.Error(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		sharedProfile.HashedPayloadUUID = uuid.NewSHA1(uuid.NameSpaceDNS, []byte(mobileconfig)).String()

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
				SaveSharedProfiles(sharedProfiles)
				if out.PushNow {
					PushSharedProfiles(devices, sharedProfiles)
				}
			} else {
				for _, item := range out.DeviceUDIDs {
					device, err := GetDevice(item)
					if err != nil {
						log.Error(err)
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}
					devices = append(devices, device)
				}
				SaveProfiles(devices, profiles)
				if out.PushNow {
					PushProfiles(devices, profiles)
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
				SaveSharedProfiles(sharedProfiles)
				if out.PushNow {
					PushSharedProfiles(devices, sharedProfiles)
				}
			} else {
				for _, item := range out.SerialNumbers {
					device, err := GetDeviceSerial(item)
					if err != nil {
						continue
					}
					devices = append(devices, device)
				}
				SaveProfiles(devices, profiles)
				if out.PushNow {
					PushProfiles(devices, profiles)
				}
			}
		}
	}
}

func DeleteProfileHandler(w http.ResponseWriter, r *http.Request) {
	var profiles []types.DeviceProfile
	var profilesModel types.DeviceProfile
	var sharedProfiles []types.SharedProfile
	var sharedProfileModel types.SharedProfile
	var devices []types.Device
	var out types.DeleteProfilePayload
	var err error
	err = json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		log.Error(err)
	}

	for _, profile := range out.Mobileconfigs {
		if out.DeviceUDIDs != nil {
			// Not empty list
			if len(out.DeviceUDIDs) > 0 {
				// Shared profiles
				if out.DeviceUDIDs[0] == "*" {
					devices, err = GetAllDevices()
					if err != nil {
						log.Error(err)
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}
					// var deviceIds []string
					// for _, item := range devices {
					// 	deviceIds = append(deviceIds, item.UDID)
					// }
					err = db.DB.Model(&sharedProfileModel).Where("payload_uuid = ? and payload_identifier = ?", profile.UUID, profile.PayloadIdentifier).Update("installed = ?", false).Update("installed", false).Scan(&sharedProfiles).Error
					if err != nil {
						log.Error(err)
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}

					DeleteSharedProfiles(devices, sharedProfiles)

				} else {
					var deviceIds []string
					for _, item := range out.DeviceUDIDs {
						device, err := GetDevice(item)
						if err != nil {
							log.Error(err)
							http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
						}
						devices = append(devices, device)
						deviceIds = append(deviceIds, device.UDID)
					}

					err := db.DB.Model(&profilesModel).Where("payload_uuid = ? and payload_identifier = ? and device_ud_id IN (?)", profile.UUID, profile.PayloadIdentifier, deviceIds).Update("installed", false).Scan(&profiles).Error
					if err != nil {
						log.Error(err)
						continue
					}

					DeleteDeviceProfiles(devices, profiles)
				}
			}
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
			log.Infof("Pushing profile to %v", device.UDID)
			commandPayload.RequestType = "InstallProfile"
			if utils.Sign() {
				priv, pub, err := loadSigningKey(utils.KeyPassword(), utils.KeyPath(), utils.CertPath())
				if err != nil {
					log.Errorf("loading signing certificate and private key: %v", err)
				}
				signed, err := SignProfile(priv, pub, profileData.MobileconfigData)
				if err != nil {
					log.Errorf("signing profile with the specified key: %v", err)
				}

				commandPayload.Payload = base64.StdEncoding.EncodeToString([]byte(signed))
			} else {
				commandPayload.Payload = base64.StdEncoding.EncodeToString([]byte(profileData.MobileconfigData))
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
			SendCommand(commandPayload)
		}
	}
}

func DeleteDeviceProfiles(devices []types.Device, profiles []types.DeviceProfile) {
	for _, device := range devices {
		for _, profileData := range profiles {
			var commandPayload types.CommandPayload
			commandPayload.UDID = device.UDID
			commandPayload.RequestType = "RemoveProfile"
			commandPayload.Identifier = profileData.PayloadIdentifier
			SendCommand(commandPayload)
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
			log.Infof("Pushing profile to %v", device.UDID)

			commandPayload.UDID = device.UDID
			commandPayload.RequestType = "InstallProfile"

			if utils.Sign() {
				priv, pub, err := loadSigningKey(utils.KeyPassword(), utils.KeyPath(), utils.CertPath())
				if err != nil {
					return pushedCommands, errors.Wrap(err, "PushSharedProfiles")
				}
				signed, err := SignProfile(priv, pub, profileData.MobileconfigData)
				if err != nil {
					return pushedCommands, errors.Wrap(err, "PushSharedProfiles")
				}

				commandPayload.Payload = base64.StdEncoding.EncodeToString([]byte(signed))
			} else {
				commandPayload.Payload = base64.StdEncoding.EncodeToString([]byte(profileData.MobileconfigData))
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

func VerifyMDMProfiles(profileListData types.ProfileListData, device types.Device) {
	log.Infof("Verifying mdm profiles for %v", device.UDID)
	var profile types.DeviceProfile
	var profiles []types.DeviceProfile
	var sharedProfile types.SharedProfile
	var sharedProfiles []types.SharedProfile
	var profilesToInstall []types.DeviceProfile
	var sharedProfilesToInstall []types.SharedProfile
	var devices []types.Device

	// Get the profiles that should be installed on the device
	err := db.DB.Model(&profile).Where("device_ud_id = ? AND installed = true", device.UDID).Scan(&profiles).Error
	if err != nil {
		log.Error(err)
	}

	// For each, loop over the present profiles
	for i := range profiles {
		savedProfile := profiles[i]
		for i := range profileListData.ProfileList {
			incomingProfile := profileListData.ProfileList[i]
			if savedProfile.HashedPayloadUUID != incomingProfile.PayloadUUID || savedProfile.PayloadIdentifier != incomingProfile.PayloadIdentifier {
				// If missing, queue up to be installed
				profilesToInstall = append(profilesToInstall, savedProfile)
			}
		}
	}

	devices = append(devices, device)
	PushProfiles(devices, profilesToInstall)

	err = db.DB.Model(&sharedProfile).Find(&sharedProfiles).Where("installed = true").Scan(&sharedProfiles).Error
	if err != nil {
		log.Error(err)
	}

	for _, savedSharedProfile := range sharedProfiles {
		found := false
		for _, incomingProfile := range profileListData.ProfileList {
			if savedSharedProfile.HashedPayloadUUID == incomingProfile.PayloadUUID && savedSharedProfile.PayloadIdentifier == incomingProfile.PayloadIdentifier {
				found = true
				continue
			}
		}

		if !found {
			sharedProfilesToInstall = append(sharedProfilesToInstall, savedSharedProfile)
		}
	}

	PushSharedProfiles(devices, sharedProfilesToInstall)
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

func GetDeviceProfilesBySerial(w http.ResponseWriter, r *http.Request) {
	var profiles []types.DeviceProfile
	vars := mux.Vars(r)

	var device types.Device

	err := db.DB.Model(&device).Where("serial_number = ?", vars["serial_number"]).First(&device).Error
	if err != nil {
		log.Errorf("Couldn't scan to Device model: %v", err)
	}

	err = db.DB.Find(&profiles).Where("device_ud_id = ?", device.UDID).Scan(&profiles).Error
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

	SendCommand(commandPayload)
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
