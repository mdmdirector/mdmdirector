package director

import (
	"encoding/json"
	intErrors "errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"gorm.io/gorm"
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
			if intErrors.Is(err, gorm.ErrRecordNotFound) {
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
			if intErrors.Is(err, gorm.ErrRecordNotFound) {
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
	err := db.DB.Model(&deviceModel).Select("is_supervised", "is_device_locator_service_enabled", "is_activation_lock_enabled", "is_do_not_disturb_in_effect", "is_cloud_backup_enabled", "system_integrity_protection_enabled", "app_analytics_enabled", "is_mdm_lost_mode_enabled", "awaiting_configuration", "diagnostic_submission_enabled", "is_multi_user").Where("ud_id = ?", newDevice.UDID).Updates(map[string]interface{}{
		"is_supervised":                       newDevice.IsSupervised,
		"is_device_locator_service_enabled":   newDevice.IsDeviceLocatorServiceEnabled,
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

	err := db.DB.Model(device).Where("serial_number = ?", serial).Order("last_checked_in desc").First(&device).Scan(&device).Error
	if err != nil {
		return device, errors.Wrap(err, "GetDeviceSerial")
	}
	return device, nil
}

func GetAllDevices() ([]types.Device, error) {
	var devices []types.Device

	err := db.DB.Preload("OSUpdateSettings").Preload("SecurityInfo").Preload("SecurityInfo.FirmwarePasswordStatus").Preload("SecurityInfo.ManagementStatus").Preload("SecurityInfo.FirewallSettings").Preload("SecurityInfo.SecureBoot").Preload("SecurityInfo.SecureBoot.SecureBootReducedSecurity").Find(&devices).Error
	if err != nil {
		return devices, errors.Wrap(err, "Get All Devices")
	}
	return devices, nil
}

func GetAllDevicesAndAssociations() ([]types.Device, error) {
	var devices []types.Device

	err := db.DB.Preload("OSUpdateSettings").Preload("SecurityInfo").Preload("SecurityInfo.FirmwarePasswordStatus").Preload("SecurityInfo.ManagementStatus").Preload("SecurityInfo.FirewallSettings").Preload("SecurityInfo.SecureBoot").Preload("SecurityInfo.SecureBoot.SecureBootReducedSecurity").Preload("Certificates").Preload("ProfileList").Find(&devices).Error
	if err != nil {
		return devices, errors.Wrap(err, "Couldn't scan to Device model from GetAllDevicesAndAssociations")
	}

	return devices, nil
}

func PostDeviceCommandHandler(w http.ResponseWriter, r *http.Request) {
	var out types.DeviceCommandPayload
	var devices []types.Device
	vars := mux.Vars(r)

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}

	command := vars["command"]
	pushNow := out.PushNow
	value := out.Value
	pin := out.Pin
	if out.DeviceUDIDs != nil {
		for i := range out.DeviceUDIDs {
			device, err := GetDevice(out.DeviceUDIDs[i])
			if err != nil {
				ErrorLogger(LogHolder{Message: err.Error()})
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
			devices = append(devices, device)
		}
	}

	if out.SerialNumbers != nil {
		for i := range out.SerialNumbers {
			device, err := GetDeviceSerial(out.SerialNumbers[i])
			if err != nil {
				ErrorLogger(LogHolder{Message: err.Error()})
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
			devices = append(devices, device)
		}
	}

	for i := range devices {
		device := devices[i]
		var deviceModel types.Device
		if command == "device_lock" {
			if pin != "" {
				err := db.DB.Model(&deviceModel).Select("lock", "unlock_pin").Where("ud_id = ?", device.UDID).Updates(map[string]interface{}{
					"lock":       value,
					"unlock_pin": pin,
				}).Error
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}
			} else {
				err := db.DB.Model(&deviceModel).Select("lock", "unlock_pin").Where("ud_id = ?", device.UDID).Updates(map[string]interface{}{
					"lock":       value,
					"unlock_pin": "",
				}).Error
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}
			}

		}

		if command == "erase_device" {
			if pin != "" {
				err := db.DB.Model(&deviceModel).Select("erase", "unlock_pin").Where("ud_id = ?", device.UDID).Updates(map[string]interface{}{
					"erase":      value,
					"unlock_pin": pin,
				}).Error
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}
			} else {
				err := db.DB.Model(&deviceModel).Select("erase", "unlock_pin").Where("ud_id = ?", device.UDID).Updates(map[string]interface{}{
					"erase":      value,
					"unlock_pin": "",
				}).Error
				if err != nil {
					ErrorLogger(LogHolder{Message: err.Error()})
				}
			}
		}

		if pushNow {
			err = EraseLockDevice(device.UDID)
			if err != nil {
				ErrorLogger(LogHolder{Message: err.Error()})
			}
		}

	}
}

func DeviceHandler(w http.ResponseWriter, r *http.Request) {
	var devices []types.Device
	var err error
	info := r.URL.Query().Get("info")
	if info == "limited" {
		devices, err = GetAllDevices()
	} else {
		devices, err = GetAllDevicesAndAssociations()
	}

	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}

	output, err := json.MarshalIndent(&devices, "", "    ")
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}

	_, err = w.Write(output)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
}

func SingleDeviceOutput(device types.Device, w http.ResponseWriter, r *http.Request) error {
	var err error
	info := r.URL.Query().Get("info")
	if info != "limited" {
		device, err = FetchDeviceAndRelations(device)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
		}
	}

	output, err := json.MarshalIndent(&device, "", "    ")
	if err != nil {
		return err
	}

	_, err = w.Write(output)
	if err != nil {
		return err
	}

	return nil
}

func SingleDeviceHandler(w http.ResponseWriter, r *http.Request) {
	var device types.Device
	vars := mux.Vars(r)

	var err error

	device, err = GetDevice(vars["udid"])
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}

	err = SingleDeviceOutput(device, w, r)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}

}

func SingleDeviceSerialHandler(w http.ResponseWriter, r *http.Request) {
	var device types.Device
	vars := mux.Vars(r)

	var err error

	device, err = GetDeviceSerial(vars["serial"])
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}

	err = SingleDeviceOutput(device, w, r)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func FetchDeviceModelAndRelations(device types.Device) (types.Device, error) {
	err := db.DB.Preload("OSUpdateSettings").Preload("SecurityInfo").Preload("SecurityInfo.FirmwarePasswordStatus").Preload("SecurityInfo.ManagementStatus").Preload("Certificates").Preload("ProfileList").Preload("Profiles").First(&device).Error
	if err != nil {
		log.Error("Couldn't scan to Device model from FetchDeviceModelAndRelations", err)
	}

	return device, nil
}

func FetchDeviceAndRelations(device types.Device) (types.Device, error) {
	var empty types.Device
	err := db.DB.Preload("OSUpdateSettings").Preload("SecurityInfo").Preload("SecurityInfo.FirmwarePasswordStatus").Preload("SecurityInfo.ManagementStatus").Preload("SecurityInfo.FirewallSettings").Preload("SecurityInfo.SecureBoot").Preload("SecurityInfo.SecureBoot.SecureBootReducedSecurity").Preload("Certificates").Preload("ProfileList").Preload("Profiles").First(&device).Error
	if err != nil {
		log.Error("Couldn't scan to Device model from FetchDeviceAndRelations", err)
	}
	if err != nil {
		return empty, errors.Wrap(err, "FetchDeviceAndRelations")
	}

	return device, nil
}

func RequestDeviceInformation(device types.Device) error {
	requestType := "DeviceInformation"
	InfoLogger(LogHolder{Message: "Requesting DeviceInfo", DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, CommandRequestType: requestType})
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
	DebugLogger(LogHolder{Message: "TokenUpdate Received", DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber})
	err := db.DB.Model(&deviceModel).Select("token_update_received", "authenticate_recieved").Where("ud_id = ?", device.UDID).Updates(map[string]interface{}{"token_update_received": true, "authenticate_recieved": true}).Error
	if err != nil {
		return device, errors.Wrap(err, "Set TokenUpdate")
	}
	updatedDevice, err := GetDevice(device.UDID)
	if err != nil {
		return device, errors.Wrap(err, "Set TokenUpdate")
	}
	return updatedDevice, nil
}

func RequestAllDeviceInfo(device types.Device) error {
	err := RequestProfileList(device)
	if err != nil {
		return errors.Wrap(err, "RequestAllDeviceInfo")
	}

	err = RequestSecurityInfo(device)
	if err != nil {
		return errors.Wrap(err, "RequestAllDeviceInfo")
	}

	err = RequestDeviceInformation(device)
	if err != nil {
		return errors.Wrap(err, "RequestAllDeviceInfo")
	}

	err = RequestCertificateList(device)
	if err != nil {
		return errors.Wrap(err, "RequestAllDeviceInfo")
	}

	return nil
}
