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
	RequestSecurityInfo(device)
	RequestDeviceInformation(device)
	InstallBootstrapPackages(device)
	SendDeviceConfigured(device)
	device.InitialTasksRun = true
	UpdateDevice(device)
}

func SendDeviceConfigured(device types.Device) {
	var commandPayload types.CommandPayload
	commandPayload.UDID = device.UDID
	commandPayload.RequestType = "DeviceConfigured"
	SendCommand(commandPayload)
}
