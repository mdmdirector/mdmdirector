package director

import (
	"errors"
	"time"

	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/log"
	"github.com/grahamgilbert/mdmdirector/types"
)

func RunInitialTasks(udid string) error {
	if udid == "" {
		return errors.New("No Device UDID")
	}
	var deviceModel types.Device
	device := GetDevice(udid)
	log.Info("Running initial tasks")
	err := ClearCommands(&device)
	if err != nil {
		return err
	}
	// RequestProfileList(device)
	InstallAllProfiles(device)
	RequestSecurityInfo(device)
	RequestDeviceInformation(device)
	InstallBootstrapPackages(device)
	SendDeviceConfigured(device)
	SaveDeviceConfigured(device)
	err = db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"last_info_requested": time.Now()}).Error
	if err != nil {
		return err
	}

	return nil
}

func SendDeviceConfigured(device types.Device) {

	var requestType = "DeviceConfigured"
	var commandPayload types.CommandPayload
	commandPayload.UDID = device.UDID
	commandPayload.RequestType = requestType
	SendCommand(commandPayload)
}

func SaveDeviceConfigured(device types.Device) error {
	var deviceModel types.Device
	err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"awaiting_configuration": false, "token_update_recieved": true, "authenticate_recieved": true, "initial_tasks_run": true}).Error
	if err != nil {
		return err
	}

	return nil
}

func ResetDevice(device types.Device) error {
	var deviceModel types.Device
	err := ClearCommands(&device)
	if err != nil {
		return err
	}
	log.Infof("Resetting %v", device.UDID)
	err = db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"token_update_recieved": false, "authenticate_recieved": false, "initial_tasks_run": false}).Error
	if err != nil {
		return err
	}

	return nil
}
