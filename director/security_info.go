package director

import (
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	log "github.com/sirupsen/logrus"
)

func RequestSecurityInfo(device types.Device) {
	requestType := "SecurityInfo"
	log.Debugf("Requesting Security Info for %v", device.UDID)
	var payload types.CommandPayload
	payload.UDID = device.UDID
	payload.RequestType = requestType
	_, err := SendCommand(payload)
	if err != nil {
		log.Error(err)
	}
}

func SaveSecurityInfo(securityInfoData types.SecurityInfoData, device types.Device) {
	var securityInfo types.SecurityInfo
	var managementStatus types.ManagementStatus
	var firmwarePasswordStatus types.FirmwarePasswordStatus
	securityInfo = securityInfoData.SecurityInfo
	managementStatus = securityInfo.ManagementStatus
	firmwarePasswordStatus = securityInfo.FirmwarePasswordStatus
	log.Infof("Saving SecurityInfo for %v", device.UDID)
	err := db.DB.Model(&device).Association("SecurityInfo").Append(&securityInfo).Error
	if err != nil {
		log.Error(err)
	}

	err = db.DB.Model(&securityInfo).Association("FirmwarePasswordStatus").Append(&firmwarePasswordStatus).Error
	if err != nil {
		log.Error(err)
	}

	err = db.DB.Model(&securityInfo).Association("ManagementStatus").Append(&managementStatus).Error
	if err != nil {
		log.Error(err)
	}
}
