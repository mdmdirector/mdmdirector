package director

import (
	"log"

	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/types"
)

func RequestSecurityInfo(device types.Device) {
	var requestType = "SecurityInfo"
	inQueue := CommandInQueue(device, requestType)
	if inQueue {
		log.Printf("%v is already in queue for %v", requestType, device.UDID)
		return
	}
	var payload types.CommandPayload
	payload.UDID = device.UDID
	payload.RequestType = requestType
	SendCommand(payload)
}

func SaveSecurityInfo(securityInfoData types.SecurityInfoData, device types.Device) {
	var securityInfo types.SecurityInfo
	var managementStatus types.ManagementStatus
	var firmwarePasswordStatus types.FirmwarePasswordStatus
	securityInfo = securityInfoData.SecurityInfo
	managementStatus = securityInfo.ManagementStatus
	firmwarePasswordStatus = securityInfo.FirmwarePasswordStatus
	err := db.DB.Model(&device).Association("SecurityInfo").Append(&securityInfo).Error
	if err != nil {
		log.Print(err)
	}

	err = db.DB.Model(&securityInfo).Association("FirmwarePasswordStatus").Append(&firmwarePasswordStatus).Error
	if err != nil {
		log.Print(err)
	}

	err = db.DB.Model(&securityInfo).Association("ManagementStatus").Append(&managementStatus).Error
	if err != nil {
		log.Print(err)
	}
}
