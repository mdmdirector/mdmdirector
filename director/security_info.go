package director

import "github.com/grahamgilbert/mdmdirector/types"

func UpdateSecurityInfo(device types.Device) {
	var payload types.CommandPayload
	payload.UDID = device.UDID
	payload.RequestType = "SecurityInfo"
	SendCommand(payload)
}
