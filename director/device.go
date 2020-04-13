package director

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/log"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/pkg/errors"

	"github.com/jinzhu/gorm"
)

func UpdateDevice(newDevice types.Device) (*types.Device, error) {
	var device types.Device
	var oldDevice types.Device

	if newDevice.UDID == "" && device.SerialNumber == "" {
		err := fmt.Errorf("No device UDID or serial set")
		return &newDevice, errors.Wrap(err, "UpdateDevice")
	}
	now := time.Now()
	// newDevice.NextPush = now.Add(12 * time.Hour)
	newDevice.LastCheckedIn = now
	if newDevice.UDID != "" {
		if err := db.DB.Where("ud_id = ?", newDevice.UDID).First(&device).Scan(&oldDevice).Error; err != nil {
			if gorm.IsRecordNotFoundError(err) {
				db.DB.Create(&newDevice)
			}
		} else {
			err := db.DB.Model(&device).Where("ud_id = ?", newDevice.UDID).Assign(&newDevice).FirstOrCreate(&device).Error
			if err != nil {
				return &newDevice, errors.Wrap(err, "Update device first or create udid")
			}
		}
	}

	if newDevice.SerialNumber != "" {
		if err := db.DB.Where("serial_number = ?", newDevice.SerialNumber).First(&device).Scan(&oldDevice).Error; err != nil {
			if gorm.IsRecordNotFoundError(err) {
				db.DB.Create(&newDevice)
			}
		} else {
			err := db.DB.Model(&device).Where("serial_number = ?", newDevice.SerialNumber).Assign(&newDevice).FirstOrCreate(&device).Error
			if err != nil {
				return &newDevice, errors.Wrap(err, "Update device first or create serial")
			}
		}
	}

	err := UpdateDeviceBools(&newDevice)
	if err != nil {
		return &device, errors.Wrap(err, "UpdateDevice")
	}

	if newDevice.AwaitingConfiguration && newDevice.InitialTasksRun {
		err := SendDeviceConfigured(newDevice)
		if err != nil {
			return &device, errors.Wrap(err, "UpdateDevice:SendDeviceConfigured")
		}
	}

	if !newDevice.InitialTasksRun && newDevice.AwaitingConfiguration {
		err := RunInitialTasks(newDevice.UDID)
		if err != nil {
			return &device, errors.Wrap(err, "UpdateDevice:RunInitialTasks")
		}
	}

	return &device, nil
}

func UpdateDeviceBools(newDevice *types.Device) error {
	var deviceModel types.Device
	err := db.DB.Model(&deviceModel).Where("ud_id = ?", newDevice.UDID).Update(map[string]interface{}{
		"is_supervised": newDevice.IsSupervised, "is_device_locator_service_enabled": newDevice.IsDeviceLocatorServiceEnabled,
		"is_activation_lock_enabled":          newDevice.IsActivationLockEnabled,
		"is_do_not_disturb_in_effect":         newDevice.IsDoNotDisturbInEffect,
		"is_cloud_backup_enabled":             newDevice.IsCloudBackupEnabled,
		"system_integrity_protection_enabled": newDevice.SystemIntegrityProtectionEnabled,
		"app_analytics_enabled":               newDevice.AppAnalyticsEnabled,
		"is_mdm_lost_mode_enabled":            newDevice.IsMDMLostModeEnabled,
		"awaiting_configuration":              newDevice.AwaitingConfiguration,
		"diagnostic_submission_enabled":       newDevice.DiagnosticSubmissionEnabled,
		"is_multi_user":                       newDevice.IsMultiUser,
	}).Error
	if err != nil {
		return err
	}

	return nil
}

func GetDevice(udid string) (types.Device, error) {
	var device types.Device

	if udid == "" {
		err := fmt.Errorf("No device UDID set")
		return device, errors.Wrap(err, "GetDevice")
	}

	err := db.DB.Model(device).Where("ud_id = ?", udid).First(&device).Scan(&device).Error
	if err != nil {
		return device, errors.Wrapf(err, "Couldn't scan to Device model from GetDevice %v", device.UDID)
	}
	return device, nil
}

func GetDeviceSerial(serial string) (types.Device, error) {
	var device types.Device

	if serial == "" {
		err := fmt.Errorf("No device Serial passed")
		return device, errors.Wrap(err, "GetDeviceSerial")
	}

	err := db.DB.Model(device).Where("serial_number = ?", serial).First(&device).Scan(&device).Error
	if err != nil {
		return device, errors.Wrap(err, "GetDeviceSerial")
	}
	return device, nil
}

func GetAllDevices() ([]types.Device, error) {
	// var device types.Device
	var devices []types.Device

	err := db.DB.Find(&devices).Scan(&devices).Error
	if err != nil {
		return devices, errors.Wrap(err, "Get All Devices")
	}
	return devices, nil
}

func GetAllDevicesAndAssociations() *[]types.Device {
	var devices []types.Device

	err := db.DB.Preload("OSUpdateSettings").Preload("SecurityInfo").Preload("SecurityInfo.FirmwarePasswordStatus").Preload("SecurityInfo.ManagementStatus").Preload("Certificates").Preload("ProfileList").Find(&devices).Error
	if err != nil {
		log.Error("Couldn't scan to Device model from GetAllDevicesAndAssociations", err)
	}

	return &devices
}

func PostDeviceCommandHandler(w http.ResponseWriter, r *http.Request) {
	var out types.DeviceCommandPayload
	var devices []types.Device
	vars := mux.Vars(r)

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		log.Error(err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}

	command := vars["command"]
	pushNow := out.PushNow
	value := out.Value
	if out.DeviceUDIDs != nil {
		for i := range out.DeviceUDIDs {
			device, err := GetDevice(out.DeviceUDIDs[i])
			if err != nil {
				log.Error(err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
			devices = append(devices, device)
		}
	}

	if out.SerialNumbers != nil {
		for i := range out.SerialNumbers {
			device, err := GetDeviceSerial(out.SerialNumbers[i])
			if err != nil {
				log.Error(err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
			devices = append(devices, device)
		}
	}

	for i := range devices {
		device := devices[i]
		var deviceModel types.Device
		if command == "device_lock" {
			err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{
				"lock": value,
			}).Error
			if err != nil {
				log.Error(err)
			}
		}

		if command == "erase_device" {
			err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{
				"erase": value,
			}).Error
			if err != nil {
				log.Error(err)
			}
		}

		if pushNow {
			dbDevice, err := GetDevice(device.UDID)
			if err != nil {
				log.Error(err)
			}
			err = EraseLockDevice(&dbDevice)
			if err != nil {
				log.Error(err)
			}
		}

	}
}

func DeviceHandler(w http.ResponseWriter, r *http.Request) {
	devices := GetAllDevicesAndAssociations()

	output, err := json.MarshalIndent(&devices, "", "    ")
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		log.Error(err)
	}
}

func SingleDeviceHandler(w http.ResponseWriter, r *http.Request) {
	var device types.Device
	vars := mux.Vars(r)

	device, err := GetDevice(vars["udid"])
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	output, err := FetchDeviceAndRelations(device)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		log.Error(err)
	}

}

func SingleDeviceSerialHandler(w http.ResponseWriter, r *http.Request) {
	var device types.Device
	vars := mux.Vars(r)

	device, err := GetDeviceSerial(vars["serial"])
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	output, err := FetchDeviceAndRelations(device)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		log.Error(err)
	}
}

func FetchDeviceAndRelations(device types.Device) ([]byte, error) {
	var empty []byte
	err := db.DB.Preload("OSUpdateSettings").Preload("SecurityInfo").Preload("SecurityInfo.FirmwarePasswordStatus").Preload("SecurityInfo.ManagementStatus").Preload("Certificates").Preload("ProfileList").First(&device).Error
	if err != nil {
		log.Error("Couldn't scan to Device model from SingleDeviceSerialHandler", err)
	}
	if err != nil {
		return empty, errors.Wrap(err, "FetchDeviceAndRelations")
	}

	output, err := json.MarshalIndent(&device, "", "    ")
	if err != nil {
		return empty, errors.Wrap(err, "FetchDeviceAndRelations")
	}

	return output, nil
}

func RequestDeviceInformation(device types.Device) error {
	requestType := "DeviceInformation"
	log.Debugf("Requesting Device Info for %v", device.UDID)
	var payload types.CommandPayload
	payload.UDID = device.UDID
	payload.RequestType = requestType
	payload.Queries = types.DeviceInformationQueries
	_, err := SendCommand(payload)
	if err != nil {
		return errors.Wrap(err, "RequestDeviceInformation:SendCommand")
	}

	return nil
}

func SetTokenUpdate(device types.Device) (types.Device, error) {
	var deviceModel types.Device
	log.Debugf("TokenUpdate received for %v", device.UDID)
	err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"token_update_received": true, "authenticate_received": true}).Error
	if err != nil {
		return device, errors.Wrap(err, "Set TokenUpdate")
	}
	updatedDevice, err := GetDevice(device.UDID)
	if err != nil {
		return device, errors.Wrap(err, "Set TokenUpdate")
	}
	return updatedDevice, nil
}
