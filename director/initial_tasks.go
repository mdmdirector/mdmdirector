package director

import (
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func RunInitialTasks(udid string) error {
	if udid == "" {
		err := errors.New("No Device UDID")
		return errors.Wrap(err, "RunInitialTasks")
	}

	device, err := GetDevice(udid)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks")
	}
	// if device.InitialTasksRun == true {
	// 	log.Infof("Initial tasks already run for %v", device.UDID)
	// 	return nil
	// }
	InfoLogger(LogHolder{Message: "Running initial tasks", DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID})
	err = ClearCommands(&device)
	if err != nil {
		return err
	}

	// if device.Erase || device.Lock {
	// 	// Got a device checking in that should be wiped or locked. Make it so.
	// 	err = EraseLockDevice(&device)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	return nil
	// }

	_, err = InstallAllProfiles(device)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks:InstallAllProfiles")
	}

	_, err = InstallBootstrapPackages(device)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks:InstallBootstrapPackages")
	}

	// commandsList := append(profileCommands, packageCommands...)
	// var uuidList []string
	// for _, command := range commandsList {
	// 	uuidList = append(uuidList, command.CommandUUID)
	// }

	err = processDeviceConfigured(device)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks:processDeviceConfigured")
	}

	return nil
}

func processDeviceConfigured(device types.Device) error {
	var deviceModel types.Device
	err := SendDeviceConfigured(device)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks")
	}
	err = SaveDeviceConfigured(device)
	if err != nil {
		return err
	}
	err = db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"last_info_requested": time.Now()}).Error
	if err != nil {
		return err
	}

	// RequestSecurityInfo(device)
	// RequestDeviceInformation(device)
	// RequestProfileList(device)
	return nil
}

func SendDeviceConfigured(device types.Device) error {
	requestType := "DeviceConfigured"
	var commandPayload types.CommandPayload
	commandPayload.UDID = device.UDID
	commandPayload.RequestType = requestType
	_, err := SendCommand(commandPayload)
	if err != nil {
		return errors.Wrap(err, "SendDeviceConfigured")
	}
	// Twice for luck
	_, err = SendCommand(commandPayload)
	if err != nil {
		return errors.Wrap(err, "SendDeviceConfigured")
	}
	return nil
}

func SaveDeviceConfigured(device types.Device) error {
	var deviceModel types.Device
	// err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"awaiting_configuration": false, "token_update_received": true, "authenticate_received": true, "initial_tasks_run": true}).Error
	err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"token_update_received": true, "authenticate_received": true, "initial_tasks_run": true}).Error
	if err != nil {
		return err
	}

	return nil
}

func ResetDevice(device types.Device) error {
	var deviceModel types.Device
	err := ClearCommands(&device)
	if err != nil {
		return errors.Wrap(err, "ResetDevice:ClearCommands")
	}
	log.Infof("Resetting %v", device.UDID)
	err = db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"token_update_received": false, "authenticate_received": false, "initial_tasks_run": false}).Error
	if err != nil {
		return errors.Wrap(err, "reset device")
	}

	return nil
}
