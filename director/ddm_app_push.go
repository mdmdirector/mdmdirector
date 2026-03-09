package director

import (
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/types"

	"github.com/pkg/errors"
)

// PushApplicationViaDDM pushes a single application via DDM declarations for a single device
// app.ID must be populated (loaded from DB) - used as declaration identifier
func PushApplicationViaDDM(client *ddm.KMFDDMClient, udid string, app types.DeviceInstallApplication) error {
	packageDeclID := ddm.PackageDeclarationID(udid, app.ID.String())
	activationDeclID := ddm.PackageActivationDeclarationID(udid, app.ID.String())

	// Step 1: PUT Package declaration (noNotify=true)
	packageDecl := ddm.Declaration{
		Identifier: packageDeclID,
		Type:       ddm.TypePackage,
		Payload: ddm.PackagePayload{
			ManifestURL: app.ManifestURL,
		},
	}

	packageChanged, err := client.PutDeclaration(packageDecl, true)
	if err != nil {
		return errors.Wrapf(err, "PushApplicationViaDDM: PUT Package declaration for %s on %s", app.ManifestURL, udid)
	}

	// If unchanged (204), touch to force reinstall
	if !packageChanged {
		if err := client.TouchDeclaration(packageDeclID, true); err != nil {
			return errors.Wrapf(err, "PushApplicationViaDDM: touch Package declaration for %s on %s", app.ManifestURL, udid)
		}
	}

	// Step 2: PUT ActivationSimple declaration referencing the Package declaration (noNotify=true)
	activationDecl := ddm.Declaration{
		Identifier: activationDeclID,
		Type:       ddm.TypeActivationSimple,
		Payload: ddm.ActivationSimplePayload{
			StandardConfigurations: []string{packageDeclID},
		},
	}

	activationChanged, err := client.PutDeclaration(activationDecl, true)
	if err != nil {
		return errors.Wrapf(err, "PushApplicationViaDDM: PUT ActivationSimple declaration for %s on %s", app.ManifestURL, udid)
	}

	// If unchanged (204), touch to force reinstall
	if !activationChanged {
		if err := client.TouchDeclaration(activationDeclID, true); err != nil {
			return errors.Wrapf(err, "PushApplicationViaDDM: touch ActivationSimple declaration for %s on %s", app.ManifestURL, udid)
		}
	}

	// Step 3: Associate Package declaration with the device's set (noNotify=true)
	if err := client.PutSetDeclaration(udid, packageDeclID, true); err != nil {
		return errors.Wrapf(err, "PushApplicationViaDDM: PUT set-declaration (package) for %s on %s", app.ManifestURL, udid)
	}

	// Step 4: Associate ActivationSimple declaration with the device's set (noNotify=true)
	if err := client.PutSetDeclaration(udid, activationDeclID, true); err != nil {
		return errors.Wrapf(err, "PushApplicationViaDDM: PUT set-declaration (activation) for %s on %s", app.ManifestURL, udid)
	}

	// Step 5: Associate enrollment with the set (noNotify=false — triggers DDM sync)
	if err := client.PutEnrollmentSet(udid, udid, false); err != nil {
		return errors.Wrapf(err, "PushApplicationViaDDM: PUT enrollment-set for %s", udid)
	}

	return nil
}

// PushApplicationsViaDDM pushes a device-specific application to all given devices via DDM
func PushApplicationsViaDDM(devices []types.Device, manifestURL string) error {
	client, err := ddm.Client()
	if err != nil {
		return err
	}

	for i := range devices {
		device := devices[i]

		var app types.DeviceInstallApplication
		if err := db.DB.Where("device_ud_id = ? AND manifest_url = ?", device.UDID, manifestURL).First(&app).Error; err != nil {
			return errors.Wrapf(err, "PushApplicationsViaDDM: querying DeviceInstallApplication for %s on %s", manifestURL, device.UDID)
		}

		InfoLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      "Pushing application via DDM",
		})

		if err := PushApplicationViaDDM(client, device.UDID, app); err != nil {
			ErrorLogger(LogHolder{
				Message:      err.Error(),
				DeviceUDID:   device.UDID,
				DeviceSerial: device.SerialNumber,
			})
			continue
		}

		InfoLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      "Pushed application via DDM",
		})
	}

	return nil
}

// PushSharedApplicationsViaDDM pushes a shared application to all given devices via DDM
func PushSharedApplicationsViaDDM(devices []types.Device, manifestURL string) error {
	client, err := ddm.Client()
	if err != nil {
		return err
	}

	var sharedApp types.SharedInstallApplication
	if err := db.DB.Where("manifest_url = ?", manifestURL).First(&sharedApp).Error; err != nil {
		return errors.Wrapf(err, "PushSharedApplicationsViaDDM: querying SharedInstallApplication for %s", manifestURL)
	}

	// Build a DeviceInstallApplication carrying the shared app's stable UUID and manifest URL
	app := types.DeviceInstallApplication{
		ID:          sharedApp.ID,
		ManifestURL: sharedApp.ManifestURL,
	}

	for i := range devices {
		device := devices[i]

		InfoLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      "Pushing shared application via DDM",
		})

		if err := PushApplicationViaDDM(client, device.UDID, app); err != nil {
			ErrorLogger(LogHolder{
				Message:      err.Error(),
				DeviceUDID:   device.UDID,
				DeviceSerial: device.SerialNumber,
			})
			continue
		}

		InfoLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      "Pushed shared application via DDM",
		})
	}

	return nil
}
