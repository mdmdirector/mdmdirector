package director

import (
	"encoding/json"
	intErrors "errors"
	"net/http"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func PostInstallApplicationHandler(w http.ResponseWriter, r *http.Request) {
	var devices []types.Device
	var out types.InstallApplicationPayload

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
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
					return
				}
				err = SaveSharedInstallApplications(out)
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}
				for _, ManifestURL := range out.ManifestURLs {
					// Push these out to existing devices right now now now
					var sharedInstallApplication types.SharedInstallApplication
					sharedInstallApplication.ManifestURL = ManifestURL.URL
					if !ManifestURL.BootstrapOnly {
						_, err = PushSharedInstallApplication(devices, sharedInstallApplication)
						if err != nil {
							ErrorLogger(LogHolder{Message: err.Error()})
						}
					}
				}
			} else {
				for _, item := range out.DeviceUDIDs {
					device, err := GetDevice(item)
					if err != nil {
						ErrorLogger(LogHolder{Message: err.Error()})
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
						return
					}
					devices = append(devices, device)
				}
				err = SaveInstallApplications(devices, out)
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}
				for _, ManifestURL := range out.ManifestURLs {
					var installApplication types.DeviceInstallApplication
					installApplication.ManifestURL = ManifestURL.URL
					if !ManifestURL.BootstrapOnly {
						_, err = PushInstallApplication(devices, installApplication)
						if err != nil {
							ErrorLogger(LogHolder{Message: err.Error()})
						}
					}
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
				err = SaveSharedInstallApplications(out)
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}
				for _, ManifestURL := range out.ManifestURLs {
					// Push these out to existing devices right now now now
					var sharedInstallApplication types.SharedInstallApplication
					sharedInstallApplication.ManifestURL = ManifestURL.URL
					if !ManifestURL.BootstrapOnly {
						_, err = PushSharedInstallApplication(devices, sharedInstallApplication)
						if err != nil {
							ErrorLogger(LogHolder{Message: err.Error()})
						}
					}
				}
			} else {
				for _, item := range out.SerialNumbers {
					device, err := GetDeviceSerial(item)
					if err != nil {
						continue
					}
					devices = append(devices, device)
				}
				for _, ManifestURL := range out.ManifestURLs {
					var installApplication types.DeviceInstallApplication
					installApplication.ManifestURL = ManifestURL.URL
					if !ManifestURL.BootstrapOnly {
						_, err = PushInstallApplication(devices, installApplication)
						if err != nil {
							ErrorLogger(LogHolder{Message: err.Error()})
						}
					}
				}
			}
		}
	}
}

func SaveInstallApplications(devices []types.Device, payload types.InstallApplicationPayload) error {
	for i := range devices {
		device := devices[i]
		for _, ManifestURL := range payload.ManifestURLs {
			installApplication := types.DeviceInstallApplication{
				ManifestURL: ManifestURL.URL,
				DeviceUDID:  device.UDID,
			}
			err := db.DB.Where("device_ud_id = ? AND manifest_url = ?", device.UDID, ManifestURL.URL).Assign(&installApplication).FirstOrCreate(&installApplication).Error
			if err != nil {
				return errors.Wrap(err, "SaveInstallApplications")
			}
		}
	}

	return nil
}

func PushInstallApplication(devices []types.Device, installApplication types.DeviceInstallApplication) ([]types.Command, error) {
	// DDM-enabled devices install via declarations; the rest via InstallApplication.
	ddmDevices, legacyDevices := partitionByDDMPackages(devices)
	var errs []error
	if len(ddmDevices) > 0 {
		if err := PushApplicationsViaDDM(ddmDevices, installApplication.ManifestURL); err != nil {
			errs = append(errs, err)
		}
	}

	var sentCommands []types.Command
	for i := range legacyDevices {
		device := legacyDevices[i]
		inQueue, err := InstallAppInQueue(device, installApplication.ManifestURL)
		if err != nil {
			// Shit went wrong for this device, but logging here feels wrong
			ErrorLogger(LogHolder{Message: err.Error()})
			continue
		}
		if inQueue {
			log.Infof("%v is already in queue for %v", installApplication.ManifestURL, device.UDID)
			continue
		}

		var commandPayload types.CommandPayload
		commandPayload.UDID = device.UDID
		commandPayload.RequestType = "InstallApplication"
		commandPayload.ManifestURL = installApplication.ManifestURL

		command, err := SendCommand(commandPayload)
		if utils.Prometheus() {
			metrics.ApplicationOperations("device", "pushed", metrics.ResultFromError(err)).Inc()
		}
		if err != nil {
			// We should return an error or something here
			ErrorLogger(LogHolder{Message: err.Error()})
			continue
		} else {
			sentCommands = append(sentCommands, command)
		}

	}
	return sentCommands, intErrors.Join(errs...)
}

func SaveSharedInstallApplications(payload types.InstallApplicationPayload) error {
	var sharedInstallApplication types.SharedInstallApplication
	if len(payload.ManifestURLs) == 0 {
		log.Debug("No manifest urls")
		return nil
	}

	for _, ManifestURL := range payload.ManifestURLs {
		sharedInstallApplication.ManifestURL = ManifestURL.URL
		err := db.DB.Model(&sharedInstallApplication).Where("manifest_url = ?", ManifestURL.URL).Assign(&sharedInstallApplication).FirstOrCreate(&sharedInstallApplication).Error
		if err != nil {
			return errors.Wrap(err, "SaveSharedInstallApplications")
		}
	}
	return nil
}

func PushSharedInstallApplication(devices []types.Device, installSharedApplication types.SharedInstallApplication) ([]types.Command, error) {
	// DDM-enabled devices install via declarations; the rest via InstallApplication.
	ddmDevices, legacyDevices := partitionByDDMPackages(devices)
	var errs []error
	if len(ddmDevices) > 0 {
		if err := PushSharedApplicationsViaDDM(ddmDevices, installSharedApplication.ManifestURL); err != nil {
			errs = append(errs, err)
		}
	}

	var sentCommands []types.Command
	for i := range legacyDevices {
		device := legacyDevices[i]
		log.Infof("Pushing InstallApplication to %v", device.UDID)
		inQueue, _ := InstallAppInQueue(device, installSharedApplication.ManifestURL)
		if inQueue {
			log.Infof("%v is already in queue for %v", installSharedApplication.ManifestURL, device.UDID)
			continue
		}

		var commandPayload types.CommandPayload
		commandPayload.UDID = device.UDID
		commandPayload.RequestType = "InstallApplication"
		commandPayload.ManifestURL = installSharedApplication.ManifestURL

		command, err := SendCommand(commandPayload)
		if utils.Prometheus() {
			metrics.ApplicationOperations("shared", "pushed", metrics.ResultFromError(err)).Inc()
		}
		if err != nil {
			errs = append(errs, errors.Wrap(err, "Push Shared Install Application"))
			return sentCommands, intErrors.Join(errs...)
		}
		sentCommands = append(sentCommands, command)

	}
	return sentCommands, intErrors.Join(errs...)
}

func InstallBootstrapPackages(device types.Device) ([]types.Command, error) {
	if ddmPackagesForDevice(device) {
		return nil, installBootstrapPackagesViaDDM(device)
	}

	var sharedInstallApplication types.SharedInstallApplication
	var deviceInstallApplication types.DeviceInstallApplication
	var sharedInstallApplications []types.SharedInstallApplication
	var deviceInstallApplications []types.DeviceInstallApplication
	var devices []types.Device
	var sentCommands []types.Command

	devices = append(devices, device)

	err := db.DB.Model(&sharedInstallApplication).Scan(&sharedInstallApplications).Error
	if err != nil {
		return sentCommands, errors.Wrap(err, "InstallBootstrapPackages:dbcall")
	}

	// Push all the apps
	for _, savedApp := range sharedInstallApplications {
		log.Debugf("InstallApplication: %v", savedApp)
		commands, err := PushSharedInstallApplication(devices, savedApp)
		if err != nil {
			return sentCommands, errors.Wrap(err, "InstallBootstrapPackages:PushSharedInstallApplication")
		}

		sentCommands = append(sentCommands, commands...)

	}

	err = db.DB.Model(&deviceInstallApplication).Where("device_ud_id = ?", device.UDID).Scan(&deviceInstallApplications).Error
	if err != nil {
		return sentCommands, errors.Wrap(err, "InstallBootstrapPackages:dbcall2")
	}

	// Push all the apps
	for _, savedApp := range deviceInstallApplications {
		log.Debugf("InstallApplication: %v", savedApp)
		commands, err := PushInstallApplication(devices, savedApp)
		if err != nil {
			return sentCommands, errors.Wrap(err, "InstallBootstrapPackages:PushInstallApplication")
		}
		sentCommands = append(sentCommands, commands...)
	}

	return sentCommands, nil
}

func installBootstrapPackagesViaDDM(device types.Device) error {
	client, err := ddm.Client()
	if err != nil {
		return err
	}

	var sharedInstallApplications []types.SharedInstallApplication
	if err := db.DB.Find(&sharedInstallApplications).Error; err != nil {
		return errors.Wrap(err, "installBootstrapPackagesViaDDM: querying shared apps")
	}

	for _, sharedApp := range sharedInstallApplications {
		log.Debugf("InstallApplication via DDM (shared): %v", sharedApp)
		app := types.DeviceInstallApplication{
			ID:          sharedApp.ID,
			ManifestURL: sharedApp.ManifestURL,
		}
		if err := PushApplicationViaDDM(client, device.UDID, app); err != nil {
			return errors.Wrapf(err, "installBootstrapPackagesViaDDM: pushing shared app %s", sharedApp.ManifestURL)
		}
	}

	var deviceInstallApplications []types.DeviceInstallApplication
	if err := db.DB.Where("device_ud_id = ?", device.UDID).Find(&deviceInstallApplications).Error; err != nil {
		return errors.Wrap(err, "installBootstrapPackagesViaDDM: querying device apps")
	}

	for _, app := range deviceInstallApplications {
		log.Debugf("InstallApplication via DDM (device): %v", app)
		if err := PushApplicationViaDDM(client, device.UDID, app); err != nil {
			return errors.Wrapf(err, "installBootstrapPackagesViaDDM: pushing device app %s", app.ManifestURL)
		}
	}

	return nil
}

func DeleteInstallApplicationHandler(w http.ResponseWriter, r *http.Request) {
	var out types.InstallApplicationPayload
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	manifestURLs := make([]string, 0, len(out.ManifestURLs))
	for _, m := range out.ManifestURLs {
		manifestURLs = append(manifestURLs, m.URL)
	}

	var sharedApps []types.SharedInstallApplication
	if err := db.DB.Where("manifest_url IN (?)", manifestURLs).Find(&sharedApps).Error; err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if len(sharedApps) == 0 {
		http.Error(w, "no matching shared install applications found", http.StatusNotFound)
		return
	}

	// Tear down declarations only for DDM-enabled devices.
	devices, err := GetAllDevices()
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ddmDevices, _ := partitionByDDMPackages(devices)
	if len(ddmDevices) == 0 {
		return
	}

	client, err := ddm.Client()
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	for _, app := range sharedApps {
		for _, device := range ddmDevices {
			if err := DeleteSharedInstallApplicationViaDDM(client, device.UDID, app); err != nil {
				ErrorLogger(LogHolder{Message: err.Error(), DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber})
			}
		}
	}

	if err := db.DB.Where("manifest_url IN (?)", manifestURLs).Delete(&types.SharedInstallApplication{}).Error; err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func GetSharedApplicationss(w http.ResponseWriter, r *http.Request) {
	var installApplications []types.SharedInstallApplication

	err := db.DB.Find(&installApplications).Scan(&installApplications).Error
	if err != nil {
		log.Error("Couldn't scan to Shared InstallApplications model", err)
	}
	output, err := json.MarshalIndent(&installApplications, "", "    ")
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
}
