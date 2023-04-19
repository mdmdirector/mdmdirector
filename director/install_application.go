package director

import (
	"encoding/json"
	"net/http"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
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
					}
					devices = append(devices, device)
					err = SaveInstallApplications(devices, out)
					if err != nil {
						ErrorLogger(LogHolder{Message: err.Error()})
					}
				}
				err = SaveInstallApplications(devices, out)
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
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

func SaveInstallApplications(devices []types.Device, installPayload types.InstallApplicationPayload) error {
	for _, device := range devices {
		for _, url := range installPayload.ManifestURLs {
			installApplication := types.DeviceInstallApplication{
				ManifestURL: url.URL,
				DeviceUDID:  device.UDID,
			}
			err := db.DB.Model(&device).
				Where("device_ud_id = ? AND manifest_url = ?", device.UDID, url.URL).
				Assign(&installApplication).
				FirstOrCreate(&installApplication).Error
			if err != nil {
				return errors.Wrap(err, "SaveInstallApplications")
			}
		}
	}
	return nil
}

// PushInstallApplication pushes an installation request for a specific application to one or more devices.
func PushInstallApplication(devices []types.Device, installApplication types.DeviceInstallApplication) ([]types.Command, error) {
	var sentCommands []types.Command

	for _, device := range devices {
		// Check if the app is already in the installation queue for this device.
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

		commandPayload := types.CommandPayload{
			UDID:        device.UDID,
			RequestType: "InstallApplication",
			ManifestURL: installApplication.ManifestURL,
		}

		command, err := SendCommand(commandPayload)
		if err != nil {
			// We should return an error or something here
			ErrorLogger(LogHolder{Message: err.Error()})
			continue
		}

		sentCommands = append(sentCommands, command)
	}
	return sentCommands, nil
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
	var sentCommands []types.Command
	for i := range devices {
		device := devices[i]
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
		if err != nil {
			return sentCommands, errors.Wrap(err, "Push Shared Install Application")
		}
		sentCommands = append(sentCommands, command)

	}
	return sentCommands, nil
}

func InstallBootstrapPackages(device types.Device) ([]types.Command, error) {
	devices := []types.Device{device}
	var (
		sharedInstallApplications []types.SharedInstallApplication
		deviceInstallApplications []types.DeviceInstallApplication
		sentCommands              []types.Command
	)

	if err := db.DB.Model(&types.SharedInstallApplication{}).Scan(&sharedInstallApplications).Error; err != nil {
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

	if err := db.DB.Model(&types.DeviceInstallApplication{}).Where("device_ud_id = ?", device.UDID).Scan(&deviceInstallApplications).Error; err != nil {
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
