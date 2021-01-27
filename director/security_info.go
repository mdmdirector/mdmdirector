package director

import (
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func RequestSecurityInfo(device types.Device) error {
	requestType := "SecurityInfo"
	log.Debugf("Requesting Security Info for %v", device.UDID)
	var payload types.CommandPayload
	payload.UDID = device.UDID
	payload.RequestType = requestType
	_, err := SendCommand(payload)
	if err != nil {
		return errors.Wrap(err, "RequestSecurityInfo: SendCommand")
	}

	return nil
}

func SaveSecurityInfo(securityInfoData types.SecurityInfoData, device types.Device) error {
	// var securityInfo types.SecurityInfo
	// var managementStatus types.ManagementStatus
	// var firmwarePasswordStatus types.FirmwarePasswordStatus
	// var firewallSettings types.FirewallSettings
	// // var firewallSettingsApplications []types.FirewallSettingsApplication
	// var secureBoot types.SecureBoot
	// var secureBootReducedSecurity types.SecureBootReducedSecurity
	// securityInfo.DeviceUDID = device.UDID
	// securityInfo = securityInfoData.SecurityInfo
	// managementStatus = securityInfo.ManagementStatus
	// managementStatus.DeviceUDID = device.UDID
	// firmwarePasswordStatus = securityInfo.FirmwarePasswordStatus
	// firmwarePasswordStatus.DeviceUDID = device.UDID
	// firewallSettings = securityInfo.FirewallSettings
	// firewallSettings.DeviceUDID = device.UDID
	// // firewallSettingsApplications = firewallSettings.FirewallSettingsApplications
	// secureBoot = securityInfo.SecureBoot
	// secureBoot.DeviceUDID = device.UDID
	// secureBootReducedSecurity = securityInfo.SecureBoot.SecureBootReducedSecurity
	// secureBootReducedSecurity.DeviceUDID = device.UDID
	// log.Info(device)

	securityInfo := securityInfoData.SecurityInfo
	securityInfo.DeviceUDID = device.UDID
	securityInfo.FirewallSettings.DeviceUDID = device.UDID
	securityInfo.FirmwarePasswordStatus.DeviceUDID = device.UDID
	securityInfo.ManagementStatus.DeviceUDID = device.UDID
	securityInfo.SecureBoot.DeviceUDID = device.UDID
	securityInfo.SecureBoot.SecureBootReducedSecurity.DeviceUDID = device.UDID
	utils.PrintStruct(securityInfo)

	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Saving SecurityInfo"})
	err := db.DB.Session(&gorm.Session{FullSaveAssociations: true}).Model(&securityInfo).Updates(&securityInfo).Error
	if err != nil {
		return errors.Wrap(err, "Replace SecurityInfo Association")
	}

	err = db.DB.Session(&gorm.Session{FullSaveAssociations: true}).Model(&securityInfo.FirmwarePasswordStatus).Updates(&securityInfo.FirmwarePasswordStatus).Error
	if err != nil {
		return errors.Wrap(err, "Append FirmwarePasswordStatus Association")
	}

	err = db.DB.Session(&gorm.Session{FullSaveAssociations: true}).Model(&securityInfo.ManagementStatus).Updates(&securityInfo.ManagementStatus).Error
	if err != nil {
		return errors.Wrap(err, "Append ManagementStatus Association")
	}

	err = db.DB.Session(&gorm.Session{FullSaveAssociations: true}).Model(&securityInfo.FirewallSettings).Updates(&securityInfo.FirewallSettings).Error
	if err != nil {
		return errors.Wrap(err, "Append FirewallSettings Association")
	}

	// err = db.DB.Unscoped().Model(&firewallSettings).Association("FirewallSettingsApplications").Replace(firewallSettingsApplications)
	// if err != nil {
	// 	ErrorLogger(LogHolder{Message: err.Error()})
	// }

	err = db.DB.Session(&gorm.Session{FullSaveAssociations: true}).Model(&securityInfo.SecureBoot).Updates(&securityInfo.SecureBoot).Error
	if err != nil {
		return errors.Wrap(err, "Append SecureBoot Association")
	}

	err = db.DB.Session(&gorm.Session{FullSaveAssociations: true}).Model(&securityInfo.SecureBoot.SecureBootReducedSecurity).Updates(&securityInfo.SecureBoot.SecureBootReducedSecurity).Error
	if err != nil {
		return errors.Wrap(err, "Append SecureBootReducedSecurity Association")
	}

	siErr := device.UpdateLastSecurityInfo()
	if siErr != nil {
		ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: siErr.Error()})
	}

	return nil
}
