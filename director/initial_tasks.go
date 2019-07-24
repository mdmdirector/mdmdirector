package director

import (
	"log"

	"github.com/grahamgilbert/mdmdirector/types"
)

func RunInitialTasks(udid string) {
	if udid == "" {
		log.Print("No Device UDID")
		return
	}
	device := GetDevice(udid)
	log.Print("Running initial tasks")
	RequestProfileList(device)
	InstallAllProfiles(device)
	RequestSecurityInfo(device)
	RequestDeviceInformation(device)
	InstallBootstrapPackages(device)
	SendDeviceConfigured(device)
	device.InitialTasksRun = true
	UpdateDevice(device)
}

func SendDeviceConfigured(device types.Device) {

	var requestType = "DeviceConfigured"
	// var deviceModel types.Device
	// inQueue := CommandInQueue(device, requestType)
	// if inQueue {
	// 	log.Printf("%v is already in queue for %v", requestType, device.UDID)
	// 	return
	// }
	device = GetDevice(device.UDID)
	var commandPayload types.CommandPayload
	commandPayload.UDID = device.UDID
	commandPayload.RequestType = requestType
	SendCommand(commandPayload)

	device.AwaitingConfiguration = false
	// utils.PrintStruct(device)
	UpdateDevice(device)
}
