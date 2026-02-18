package director

import (
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"

	"github.com/pkg/errors"
)

// PushProfileViaDDM pushes a single profile via DDM declarations
func PushProfileViaDDM(client *ddm.KMFDDMClient, udid string, payloadIdentifier string, nanoMDMURL string) error {
	profileID := ddm.ProfileIDFromPayloadIdentifier(payloadIdentifier)
	legacyDeclID := ddm.LegacyProfileDeclarationID(udid, profileID)
	activationDeclID := ddm.ActivationDeclarationID(udid, profileID)
	profileURL := ddm.ProfileDownloadURL(nanoMDMURL, udid, payloadIdentifier)

	// Step 1: PUT LegacyProfile declaration (noNotify=true)
	legacyDecl := ddm.Declaration{
		Identifier: legacyDeclID,
		Type:       ddm.TypeLegacyProfile,
		Payload: ddm.LegacyProfilePayload{
			ProfileURL: profileURL,
		},
	}

	legacyChanged, err := client.PutDeclaration(legacyDecl, true)
	if err != nil {
		return errors.Wrapf(err, "PushProfileViaDDM: PUT LegacyProfile declaration for %s on %s", payloadIdentifier, udid)
	}

	// If unchanged (204), touch to force reinstall
	if !legacyChanged {
		if err := client.TouchDeclaration(legacyDeclID, true); err != nil {
			return errors.Wrapf(err, "PushProfileViaDDM: touch LegacyProfile declaration for %s on %s", payloadIdentifier, udid)
		}
	}

	// Step 2: PUT ActivationSimple declaration (noNotify=true)
	activationDecl := ddm.Declaration{
		Identifier: activationDeclID,
		Type:       ddm.TypeActivationSimple,
		Payload: ddm.ActivationSimplePayload{
			StandardConfigurations: []string{legacyDeclID},
		},
	}

	activationChanged, err := client.PutDeclaration(activationDecl, true)
	if err != nil {
		return errors.Wrapf(err, "PushProfileViaDDM: PUT ActivationSimple declaration for %s on %s", payloadIdentifier, udid)
	}

	// If unchanged (204), touch to force reinstall
	if !activationChanged {
		if err := client.TouchDeclaration(activationDeclID, true); err != nil {
			return errors.Wrapf(err, "PushProfileViaDDM: touch ActivationSimple declaration for %s on %s", payloadIdentifier, udid)
		}
	}

	// Step 3: Associate LegacyProfile declaration with the device's set (noNotify=true)
	if err := client.PutSetDeclaration(udid, legacyDeclID, true); err != nil {
		return errors.Wrapf(err, "PushProfileViaDDM: PUT set-declaration (legacy) for %s on %s", payloadIdentifier, udid)
	}

	// Step 4: Associate ActivationSimple declaration with the device's set (noNotify=true)
	if err := client.PutSetDeclaration(udid, activationDeclID, true); err != nil {
		return errors.Wrapf(err, "PushProfileViaDDM: PUT set-declaration (activation) for %s on %s", payloadIdentifier, udid)
	}

	// Step 5: Associate enrollment with the set (noNotify=false — triggers DDM sync)
	if err := client.PutEnrollmentSet(udid, udid, false); err != nil {
		return errors.Wrapf(err, "PushProfileViaDDM: PUT enrollment-set for %s", udid)
	}

	return nil
}

// PushProfilesViaDDM pushes device-specific profiles via DDM declarations
func PushProfilesViaDDM(devices []types.Device, profiles []types.DeviceProfile) error {
	client, err := ddm.Client()
	if err != nil {
		return err
	}

	nanoMDMURL := utils.NanoMDMURL()

	for i := range devices {
		device := devices[i]
		for j := range profiles {
			profileData := profiles[j]

			InfoLogger(
				LogHolder{
					DeviceUDID:        device.UDID,
					DeviceSerial:      device.SerialNumber,
					Message:           "Pushing Device Profile via DDM",
					ProfileIdentifier: profileData.PayloadIdentifier,
				},
			)

			if err := PushProfileViaDDM(client, device.UDID, profileData.PayloadIdentifier, nanoMDMURL); err != nil {
				ErrorLogger(LogHolder{
					Message:           err.Error(),
					DeviceUDID:        device.UDID,
					DeviceSerial:      device.SerialNumber,
					ProfileIdentifier: profileData.PayloadIdentifier,
				})
				continue
			}

			InfoLogger(
				LogHolder{
					DeviceUDID:        device.UDID,
					DeviceSerial:      device.SerialNumber,
					Message:           "Pushed Device Profile via DDM",
					ProfileIdentifier: profileData.PayloadIdentifier,
				},
			)
		}
	}

	return nil
}

// PushSharedProfilesViaDDM pushes shared profiles via DDM declarations
func PushSharedProfilesViaDDM(devices []types.Device, profiles []types.SharedProfile) error {
	client, err := ddm.Client()
	if err != nil {
		return err
	}

	nanoMDMURL := utils.NanoMDMURL()

	for i := range profiles {
		profileData := profiles[i]

		// get devices that have a device-specific version of this profile
		skipUDIDs, err := getDeviceSpecificProfileUDIDs(profileData.PayloadIdentifier)
		if err != nil {
			return errors.Wrap(err, "PushSharedProfilesViaDDM: could not query device-specific profiles")
		}

		for j := range devices {
			device := devices[j]
			// skip pushing shared profile if device has a device-specific version
			if _, ok := skipUDIDs[device.UDID]; ok {
				continue
			}

			InfoLogger(
				LogHolder{
					DeviceUDID:        device.UDID,
					DeviceSerial:      device.SerialNumber,
					Message:           "Pushing Shared Profile via DDM",
					ProfileIdentifier: profileData.PayloadIdentifier,
				},
			)

			if err := PushProfileViaDDM(client, device.UDID, profileData.PayloadIdentifier, nanoMDMURL); err != nil {
				ErrorLogger(LogHolder{
					Message:           err.Error(),
					DeviceUDID:        device.UDID,
					DeviceSerial:      device.SerialNumber,
					ProfileIdentifier: profileData.PayloadIdentifier,
				})
				continue
			}

			InfoLogger(
				LogHolder{
					DeviceUDID:        device.UDID,
					DeviceSerial:      device.SerialNumber,
					Message:           "Pushed Shared Profile via DDM",
					ProfileIdentifier: profileData.PayloadIdentifier,
				},
			)
		}
	}

	return nil
}

// DeleteProfileViaDDM removes a single profile's DDM declarations for a device
func DeleteProfileViaDDM(client *ddm.KMFDDMClient, udid string, payloadIdentifier string) error {
	profileID := ddm.ProfileIDFromPayloadIdentifier(payloadIdentifier)
	legacyDeclID := ddm.LegacyProfileDeclarationID(udid, profileID)
	activationDeclID := ddm.ActivationDeclarationID(udid, profileID)

	// Step 1: Remove LegacyProfile from the device's set (noNotify=true)
	if err := client.DeleteSetDeclaration(udid, legacyDeclID, true); err != nil {
		return errors.Wrapf(err, "DeleteProfileViaDDM: DELETE set-declaration (legacy) for %s on %s", payloadIdentifier, udid)
	}

	// Step 2: Remove ActivationSimple from the device's set (noNotify=true)
	if err := client.DeleteSetDeclaration(udid, activationDeclID, true); err != nil {
		return errors.Wrapf(err, "DeleteProfileViaDDM: DELETE set-declaration (activation) for %s on %s", payloadIdentifier, udid)
	}

	// Step 3: Delete LegacyProfile declaration (noNotify=true)
	if err := client.DeleteDeclaration(legacyDeclID, true); err != nil {
		return errors.Wrapf(err, "DeleteProfileViaDDM: DELETE declaration (legacy) for %s on %s", payloadIdentifier, udid)
	}

	// Step 4: Delete ActivationSimple declaration (noNotify=true)
	if err := client.DeleteDeclaration(activationDeclID, true); err != nil {
		return errors.Wrapf(err, "DeleteProfileViaDDM: DELETE declaration (activation) for %s on %s", payloadIdentifier, udid)
	}

	// Step 5: Re-associate enrollment with set (noNotify=false — triggers DDM sync)
	if err := client.PutEnrollmentSet(udid, udid, false); err != nil {
		return errors.Wrapf(err, "DeleteProfileViaDDM: PUT enrollment-set for %s", udid)
	}

	return nil
}

// DeleteDeviceProfilesViaDDM removes device-specific profiles via DDM declaration deletion
func DeleteDeviceProfilesViaDDM(devices []types.Device, profiles []types.DeviceProfile) error {
	client, err := ddm.Client()
	if err != nil {
		return err
	}

	for i := range devices {
		device := devices[i]
		for j := range profiles {
			profileData := profiles[j]

			InfoLogger(
				LogHolder{
					DeviceUDID:        device.UDID,
					DeviceSerial:      device.SerialNumber,
					Message:           "Deleting Device Profile via DDM",
					ProfileIdentifier: profileData.PayloadIdentifier,
				},
			)

			if err := DeleteProfileViaDDM(client, device.UDID, profileData.PayloadIdentifier); err != nil {
				ErrorLogger(LogHolder{
					Message:           err.Error(),
					DeviceUDID:        device.UDID,
					DeviceSerial:      device.SerialNumber,
					ProfileIdentifier: profileData.PayloadIdentifier,
				})
				continue
			}

			InfoLogger(
				LogHolder{
					DeviceUDID:        device.UDID,
					DeviceSerial:      device.SerialNumber,
					Message:           "Deleted Device Profile via DDM",
					ProfileIdentifier: profileData.PayloadIdentifier,
				},
			)
		}
	}

	return nil
}

// DeleteSharedProfilesViaDDM removes shared profiles via DDM declaration deletion
// skips devices that have a device-specific version of the profile
func DeleteSharedProfilesViaDDM(devices []types.Device, profiles []types.SharedProfile) error {
	client, err := ddm.Client()
	if err != nil {
		return err
	}

	for i := range profiles {
		profileData := profiles[i]

		// get devices that have a device-specific version of this profile
		skipUDIDs, err := getDeviceSpecificProfileUDIDs(profileData.PayloadIdentifier)
		if err != nil {
			return errors.Wrap(err, "DeleteSharedProfilesViaDDM: could not query device-specific profiles")
		}

		for j := range devices {
			device := devices[j]
			// skip deleting shared profile if device has a device-specific version
			if _, ok := skipUDIDs[device.UDID]; ok {
				continue
			}

			InfoLogger(
				LogHolder{
					DeviceUDID:        device.UDID,
					DeviceSerial:      device.SerialNumber,
					Message:           "Deleting Shared Profile via DDM",
					ProfileIdentifier: profileData.PayloadIdentifier,
				},
			)

			if err := DeleteProfileViaDDM(client, device.UDID, profileData.PayloadIdentifier); err != nil {
				ErrorLogger(LogHolder{
					Message:           err.Error(),
					DeviceUDID:        device.UDID,
					DeviceSerial:      device.SerialNumber,
					ProfileIdentifier: profileData.PayloadIdentifier,
				})
				continue
			}

			InfoLogger(
				LogHolder{
					DeviceUDID:        device.UDID,
					DeviceSerial:      device.SerialNumber,
					Message:           "Deleted Shared Profile via DDM",
					ProfileIdentifier: profileData.PayloadIdentifier,
				},
			)
		}
	}

	return nil
}

// getDeviceSpecificProfileUDIDs returns a set of device UDIDs that have a device-specific version of profile
func getDeviceSpecificProfileUDIDs(payloadIdentifier string) (map[string]struct{}, error) {
	var skipProfileDevices []types.DeviceProfile
	err := db.DB.Select("device_ud_id").Where("payload_identifier = ?", payloadIdentifier).Find(&skipProfileDevices).Error
	if err != nil {
		return nil, err
	}
	skipUDIDs := make(map[string]struct{})
	for _, deviceProfile := range skipProfileDevices {
		skipUDIDs[deviceProfile.DeviceUDID] = struct{}{}
	}
	return skipUDIDs, nil
}
