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
	"log"
	"net/http"
	"path/filepath"

	"github.com/fullsailor/pkcs7"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/types"
	"github.com/grahamgilbert/mdmdirector/utils"
	"github.com/groob/plist"
	"github.com/pkg/errors"
	"golang.org/x/crypto/pkcs12"
)

func PostProfileHandler(w http.ResponseWriter, r *http.Request) {
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

		var tempProfileDict map[string]interface{}
		err = plist.Unmarshal(mobileconfig, &tempProfileDict)
		if err != nil {
			log.Print(err)
		}

		profile.HashedPayloadUUID = uuid.NewSHA1(uuid.NameSpaceDNS, []byte(mobileconfig)).String()

		tempProfileDict["PayloadUUID"] = profile.HashedPayloadUUID

		mobileconfig, err = plist.MarshalIndent(&tempProfileDict, "\t")
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

		sharedProfile.HashedPayloadUUID = uuid.NewSHA1(uuid.NameSpaceDNS, []byte(mobileconfig)).String()

		var sharedTempProfileDict map[string]interface{}
		err = plist.Unmarshal(mobileconfig, &sharedTempProfileDict)
		if err != nil {
			log.Print(err)
		}

		sharedTempProfileDict["PayloadUUID"] = sharedProfile.HashedPayloadUUID

		mobileconfig, err = plist.MarshalIndent(&sharedTempProfileDict, "\t")
		if err != nil {
			log.Print(err)
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
				devices = GetAllDevices()
				SaveSharedProfiles(sharedProfiles)
				PushSharedProfiles(devices, sharedProfiles)
			} else {
				for _, item := range out.DeviceUDIDs {
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
				SaveSharedProfiles(sharedProfiles)
				PushSharedProfiles(devices, sharedProfiles)
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

func DeleteProfileHandler(w http.ResponseWriter, r *http.Request) {
	var profiles []types.DeviceProfile
	var profilesModel types.DeviceProfile
	var sharedProfiles []types.SharedProfile
	var sharedProfileModel types.SharedProfile
	var devices []types.Device
	var out types.DeleteProfilePayload

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		log.Print(err)
	}

	for _, profile := range out.Mobileconfigs {
		if out.DeviceUDIDs != nil {
			// Not empty list
			if len(out.DeviceUDIDs) > 0 {
				// Shared profiles
				if out.DeviceUDIDs[0] == "*" {
					var devices = GetAllDevices()
					var deviceIds []string
					for _, item := range devices {
						deviceIds = append(deviceIds, item.UDID)
					}
					err := db.DB.Model(&sharedProfileModel).Where("payload_uuid = ? and payload_identifier = ?", profile.UUID, profile.PayloadIdentifier).Update("installed = ?", false).Update("installed", false).Scan(&sharedProfiles).Error
					if err != nil {
						log.Print(err)
						continue
					}

					DeleteSharedProfiles(devices, sharedProfiles)

				} else {
					var deviceIds []string
					for _, item := range out.DeviceUDIDs {
						device := GetDevice(item)
						devices = append(devices, device)
						deviceIds = append(deviceIds, device.UDID)
					}

					err := db.DB.Model(&profilesModel).Where("payload_uuid = ? and payload_identifier = ? and device_ud_id IN (?)", profile.UUID, profile.PayloadIdentifier, deviceIds).Update("installed", false).Scan(&profiles).Error
					if err != nil {
						log.Print(err)
						continue
					}

					DeleteDeviceProfiles(devices, profiles)
				}

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

			commandPayload.RequestType = "InstallProfile"
			if utils.Sign() == true {
				priv, pub, err := loadSigningKey(utils.KeyPassword(), utils.KeyPath(), utils.CertPath())
				if err != nil {
					log.Printf("loading signing certificate and private key: %v", err)
				}
				signed, err := SignProfile(priv, pub, profileData.MobileconfigData)
				if err != nil {
					log.Printf("signing profile with the specified key: %v", err)
				}

				commandPayload.Payload = base64.StdEncoding.EncodeToString([]byte(signed))
			} else {
				commandPayload.Payload = base64.StdEncoding.EncodeToString([]byte(profileData.MobileconfigData))
			}

			commandPayload.UDID = device.UDID

			SendCommand(commandPayload)

		}
	}
}

func SaveSharedProfiles(profiles []types.SharedProfile) {
	var profile types.SharedProfile
	if len(profiles) == 0 {
		return
	}
	tx := db.DB.Model(&profile)
	for _, profileData := range profiles {
		if profileData.PayloadIdentifier != "" {
			tx = tx.Where("payload_identifier = ?", profileData.PayloadIdentifier)
		}
	}
	err := tx.Delete(&profile).Error
	if err != nil {
		fmt.Print(err)
	}
	tx2 := db.DB.Model(&profile)
	for _, profileData := range profiles {
		tx2 = tx2.Create(&profileData)
	}

	err = tx2.Error
	if err != nil {
		fmt.Print(err)
	}
	// db.DB.Create(&profiles)
}

func DeleteSharedProfiles(devices []types.Device, profiles []types.SharedProfile) {
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

func DeleteDeviceProfiles(devices []types.Device, profiles []types.DeviceProfile) {
	log.Print("in DeleteDeviceProfiles")
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

func PushSharedProfiles(devices []types.Device, profiles []types.SharedProfile) {
	for _, device := range devices {

		for _, profileData := range profiles {
			var commandPayload types.CommandPayload
			// var jsonString []byte
			commandPayload.UDID = device.UDID
			commandPayload.RequestType = "InstallProfile"

			if utils.Sign() == true {
				priv, pub, err := loadSigningKey(utils.KeyPassword(), utils.KeyPath(), utils.CertPath())
				if err != nil {
					log.Printf("loading signing certificate and private key: %v", err)
				}
				signed, err := SignProfile(priv, pub, profileData.MobileconfigData)
				if err != nil {
					log.Printf("signing profile with the specified key: %v", err)
				}

				commandPayload.Payload = base64.StdEncoding.EncodeToString([]byte(signed))
			} else {
				commandPayload.Payload = base64.StdEncoding.EncodeToString([]byte(profileData.MobileconfigData))
			}

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
	err := db.DB.Model(&profile).Where("device_ud_id = ? AND installed = true", device.UDID).Scan(&profiles).Error
	if err != nil {
		log.Print(err)
	}

	// For each, loop over the present profiles
	for _, savedProfile := range profiles {
		for _, incomingProfile := range profileListData.ProfileList {
			if savedProfile.HashedPayloadUUID != incomingProfile.PayloadUUID || savedProfile.PayloadIdentifier != incomingProfile.PayloadIdentifier {
				// If missing, queue up to be installed
				profilesToInstall = append(profilesToInstall, savedProfile)
			}
		}
	}

	devices = append(devices, device)
	ProcessProfiles(devices, profilesToInstall)

	err = db.DB.Model(&sharedProfile).Find(&sharedProfiles).Where("installed = true").Scan(&sharedProfiles).Error
	if err != nil {
		log.Print(err)
	}

	for _, savedSharedProfile := range sharedProfiles {
		var found = false
		for _, incomingProfile := range profileListData.ProfileList {
			if savedSharedProfile.HashedPayloadUUID == incomingProfile.PayloadUUID && savedSharedProfile.HashedPayloadUUID == incomingProfile.PayloadUUID {
				found = true
				continue
			}
		}

		if found == false {
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
		fmt.Println(err)
		log.Print("Couldn't scan to Device model")
	}
	output, err := json.MarshalIndent(&profiles, "", "    ")
	if err != nil {
		log.Print(err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	w.Write(output)

}

// Sign takes an unsigned payload and signs it with the provided private key and certificate.
func SignProfile(key crypto.PrivateKey, cert *x509.Certificate, mobileconfig []byte) ([]byte, error) {
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
		b, err := x509.DecryptPEMBlock(keyDataBlock, []byte(keyPass))
		if err != nil {
			return nil, nil, fmt.Errorf("decrypting DES private key %s", err)
		}
		pemKeyData = b
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
	var requestType = "ProfileList"
	inQueue := CommandInQueue(device, requestType)
	if inQueue {
		log.Printf("%v is already in queue for %v", requestType, device.UDID)
		return
	}

	var commandPayload types.CommandPayload
	commandPayload.UDID = device.UDID
	commandPayload.RequestType = requestType

	SendCommand(commandPayload)
}
