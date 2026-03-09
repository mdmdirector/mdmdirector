package director

import (
	"crypto/x509"
	"encoding/base64"
	"os"
	"time"

	"github.com/fullsailor/pkcs7"
	"github.com/groob/plist"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
)

func reinstallEnrollmentProfile(device types.Device) error {
	enrollmentProfile := utils.EnrollmentProfile()
	data, err := os.ReadFile(enrollmentProfile)
	if err != nil {
		return errors.Wrap(err, "Failed to read enrollment profile")
	}

	var profile types.DeviceProfile

	InfoLogger(
		LogHolder{
			DeviceSerial: device.SerialNumber,
			DeviceUDID:   device.UDID,
			Message:      "Pushing new enrollment profile",
		},
	)

	if utils.SignedEnrollmentProfile() {
		DebugLogger(
			LogHolder{
				DeviceUDID:   device.UDID,
				DeviceSerial: device.SerialNumber,
				Message:      "Enrollment Profile pre-signed",
			},
		)
		pkcs7Data, err := pkcs7.Parse(data)
		if err != nil {
			return errors.Wrap(
				err,
				"Failed to parse certificate information from signed enrollment profile",
			)
		}

		for _, cert := range pkcs7Data.Certificates {
			now := time.Now()
			if now.After(cert.NotAfter) {
				err = errors.New("Certificate used to sign enrollment profile is expired")
				return err
			}

			if now.Before(cert.NotBefore) {
				err = errors.New("Certificate used to sign enrollment profile is not yet valid")
				return err
			}
		}
		var commandPayload types.CommandPayload
		commandPayload.RequestType = "InstallProfile"
		commandPayload.Payload = base64.StdEncoding.EncodeToString(data)
		commandPayload.UDID = device.UDID

		_, err = SendCommand(commandPayload)
		if err != nil {
			return errors.Wrap(err, "Failed to push enrollment profile")
		}
	} else {
		DebugLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Signing Enrollment Profile"})
		err = plist.Unmarshal(data, &profile)
		if err != nil {
			return errors.Wrap(err, "Failed to unmarshal enrollment profile to struct")
		}

		profile.MobileconfigData = data

		// enrollment profile cannot be installed using DDM
		_, err = PushProfiles([]types.Device{device}, []types.DeviceProfile{profile}, false)
		if err != nil {
			return errors.Wrap(err, "Failed to push enrollment profile")
		}
	}
	return nil
}

// If we have enabled signing profiles, this function will verify that the certificate used to sign the enrollment profile is the same as we have locally, and if it is not, will reinstall the profile
func ensureCertOnEnrollmentProfile(
	device types.Device,
	profileLists []types.ProfileList,
	signingCert *x509.Certificate,
) error {
	// Return early if we don't want to sign
	if !utils.Sign() {
		return nil
	}

	for i := range profileLists {
		for j := range profileLists[i].PayloadContent {
			if profileLists[i].PayloadContent[j].PayloadType == "com.apple.mdm" {
				profileForVerification := ProfileForVerification{
					PayloadUUID:       profileLists[i].PayloadUUID,
					PayloadIdentifier: profileLists[i].PayloadIdentifier,
					HashedPayloadUUID: profileLists[i].PayloadUUID,
					DeviceUDID:        device.UDID,
					Installed:         true, // You always want an enrollment profile to be installed
				}

				_, needsReinstall, err := validateProfileInProfileList(
					profileForVerification,
					profileLists,
					signingCert,
				)
				if err != nil {
					return errors.Wrap(err, "validateProfileInProfileList")
				}

				if needsReinstall {
					err = reinstallEnrollmentProfile(device)
					if err != nil {
						return errors.Wrap(err, "reinstallEnrollmentProfile")
					}
				}

				return nil
			}
		}

	}

	return nil
}
