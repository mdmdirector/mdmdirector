package director

import (
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/log"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/pkg/errors"
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
	log.Info("Running initial tasks")
	err = ClearCommands(&device)
	if err != nil {
		return err
	}

	profileCommands, err := InstallAllProfiles(device)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks:InstallAllProfiles")
	}

	packageCommands, err := InstallBootstrapPackages(device)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks:InstallBootstrapPackages")
	}

	commandsList := append(profileCommands, packageCommands...)
	var uuidList []string
	for _, command := range commandsList {
		uuidList = append(uuidList, command.CommandUUID)
	}

	err = processDeviceConfigured(uuidList, device)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks:processDeviceConfigured")
	}

	return nil
}

func processDeviceConfigured(uuidList []string, device types.Device) error {
	// var commandModel types.Command
	var deviceModel types.Device
	// This would be cool, but I think we're hitting a race condition somewhere
	// for {
	// 	var unackedCommands []types.Command
	// 	if err := db.DB.Model(&commandModel).Where("(status = ? OR status = ?) AND command_uuid IN (?)", "", "NotNow", uuidList).Scan(&unackedCommands).Error; err != nil {
	// 		if gorm.IsRecordNotFoundError(err) {
	// 			log.Debug("No commands that are unacked")
	// 			break
	// 		} else {
	// 			log.Debug(err)

	// 		}
	// 	} else {
	// 		if len(unackedCommands) == 0 {
	// 			log.Debug("No commands that are unacked")
	// 			break
	// 		}
	// 		log.Debug("uacked commands found. Sleeping 1 second")
	// 		log.Debug(unackedCommands)
	// 		time.Sleep(1 * time.Second)
	// 	}
	// }

	err := SendDeviceConfigured(device)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks")
	}
	SaveDeviceConfigured(device)
	err = db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"last_info_requested": time.Now()}).Error
	if err != nil {
		return err
	}

	RequestSecurityInfo(device)
	RequestDeviceInformation(device)
	RequestProfileList(device)
	return nil
}

func SendDeviceConfigured(device types.Device) error {

	var requestType = "DeviceConfigured"
	var commandPayload types.CommandPayload
	commandPayload.UDID = device.UDID
	commandPayload.RequestType = requestType
	_, err := SendCommand(commandPayload)
	if err != nil {
		return errors.Wrap(err, "SendDeviceConfigured")
	}
	return nil
}

func SaveDeviceConfigured(device types.Device) error {
	var deviceModel types.Device
	// err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"awaiting_configuration": false, "token_update_recieved": true, "authenticate_recieved": true, "initial_tasks_run": true}).Error
	err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"token_update_recieved": true, "authenticate_recieved": true, "initial_tasks_run": true}).Error
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
	err = db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"token_update_recieved": false, "authenticate_recieved": false, "initial_tasks_run": false}).Error
	if err != nil {
		return errors.Wrap(err, "reset device")
	}

	return nil
}
