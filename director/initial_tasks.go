package director

import (
	"log"

	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/types"
)

func RunInitialTasks(udid string) {
	if udid == "" {
		log.Print("No Device UDID")
		return
	}
	device := GetDevice(udid)
	log.Print("Running initial tasks")
	ClearCommands(&device)
	RequestProfileList(device)
	InstallAllProfiles(device)
	RequestSecurityInfo(device)
	RequestDeviceInformation(device)
	InstallBootstrapPackages(device)
	SendDeviceConfigured(device)
	SaveDeviceConfigured(device)
}

func SendDeviceConfigured(device types.Device) {

	var requestType = "DeviceConfigured"
	// var deviceModel types.Device
	// inQueue := CommandInQueue(device, requestType)
	// if inQueue {
	// 	log.Printf("%v is already in queue for %v", requestType, device.UDID)
	// 	return
	// }
	// savedDevice := GetDevice(device.UDID)
	var commandPayload types.CommandPayload
	commandPayload.UDID = device.UDID
	commandPayload.RequestType = requestType
	SendCommand(commandPayload)
}

func SaveDeviceConfigured(device types.Device) {
	var deviceModel types.Device
	err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"awaiting_configuration": false, "token_update_recieved": true, "authenticate_recieved": true, "initial_tasks_run": true}).Error
	if err != nil {
		log.Print(err)
	}
}

func ResetDevice(device types.Device) {
	var deviceModel types.Device
	ClearCommands(&device)
	log.Printf("Resetting %v", device.UDID)
	err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Update(map[string]interface{}{"token_update_recieved": false, "authenticate_recieved": false, "initial_tasks_run": false}).Error
	if err != nil {
		log.Print(err)
	}
}
