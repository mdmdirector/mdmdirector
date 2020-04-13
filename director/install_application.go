package director

import (
	"encoding/json"
	"net/http"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/log"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/pkg/errors"
)

func PostInstallApplicationHandler(w http.ResponseWriter, r *http.Request) {
	// var deviceApplications []types.DeviceInstallApplication
	// var sharedApplications []types.SharedInstallApplication
	var devices []types.Device
	var out types.InstallApplicationPayload

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		log.Error(err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
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
				err = SaveSharedInstallApplications(out)
				if err != nil {
					log.Error(err)
				}
				for _, ManifestURL := range out.ManifestURLs {
					// Push these out to existing devices right now now now
					var sharedInstallApplication types.SharedInstallApplication
					sharedInstallApplication.ManifestURL = ManifestURL.URL
					if !ManifestURL.BootstrapOnly {
						_, err = PushSharedInstallApplication(devices, sharedInstallApplication)
						if err != nil {
							log.Error(err)
						}
					}
				}
			} else {
				for _, item := range out.DeviceUDIDs {
					device, err := GetDevice(item)
					if err != nil {
						log.Error(err)
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}
					devices = append(devices, device)
					err = SaveInstallApplications(devices, out)
					if err != nil {
						log.Error(err)
					}
				}
				err = SaveInstallApplications(devices, out)
				if err != nil {
					log.Error(err)
				}
				for _, ManifestURL := range out.ManifestURLs {
					var installApplication types.DeviceInstallApplication
					installApplication.ManifestURL = ManifestURL.URL
					if !ManifestURL.BootstrapOnly {
						_, err = PushInstallApplication(devices, installApplication)
						if err != nil {
							log.Error(err)
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
					log.Error(err)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				err = SaveSharedInstallApplications(out)
				if err != nil {
					log.Error(err)
				}
				for _, ManifestURL := range out.ManifestURLs {
					// Push these out to existing devices right now now now
					var sharedInstallApplication types.SharedInstallApplication
					sharedInstallApplication.ManifestURL = ManifestURL.URL
					if !ManifestURL.BootstrapOnly {
						_, err = PushSharedInstallApplication(devices, sharedInstallApplication)
						if err != nil {
							log.Error(err)
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
							log.Error(err)
						}
					}
				}
			}
		}
	}
}

func SaveInstallApplications(devices []types.Device, payload types.InstallApplicationPayload) error {
	var installApplication types.DeviceInstallApplication

	for i := range devices {
		device := devices[i]
		for _, ManifestURL := range payload.ManifestURLs {
			installApplication.ManifestURL = ManifestURL.URL
			installApplication.DeviceUDID = device.UDID
			err := db.DB.Model(&device).Where("device_ud_id = ? AND manifest_url = ?", device.UDID, ManifestURL.URL).Assign(&installApplication).FirstOrCreate(&installApplication).Error
			if err != nil {
				return errors.Wrap(err, "SaveInstallApplications")
			}
		}
	}

	return nil
}

func PushInstallApplication(devices []types.Device, installApplication types.DeviceInstallApplication) ([]types.Command, error) {
	var sentCommands []types.Command
	for i := range devices {
		device := devices[i]
		inQueue, err := InstallAppInQueue(device, installApplication.ManifestURL)
		if err != nil {
			// Shit went wrong for this device, but logging here feels wrong
			log.Error(err)
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
		if err != nil {
			// We should return an error or something here
			log.Error(err)
			continue
		} else {
			sentCommands = append(sentCommands, command)
		}

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

func GetSharedApplicationss(w http.ResponseWriter, r *http.Request) {
	var installApplications []types.SharedInstallApplication

	err := db.DB.Find(&installApplications).Scan(&installApplications).Error
	if err != nil {
		log.Error("Couldn't scan to Shared InstallApplications model", err)
	}
	output, err := json.MarshalIndent(&installApplications, "", "    ")
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		log.Error(err)
	}
}
