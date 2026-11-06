package director

import (
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/pkg/errors"

	"gorm.io/gorm"
)

func RequestSecurityInfo(device types.Device) error {
	return requestInfoCommand(device, "SecurityInfo", nil, nil)
}

func SaveSecurityInfo(securityInfoData types.SecurityInfoData, device types.Device) error {
	securityInfo := securityInfoData.SecurityInfo
	securityInfo.DeviceUDID = device.UDID
	securityInfo.FirewallSettings.DeviceUDID = device.UDID
	securityInfo.FirmwarePasswordStatus.DeviceUDID = device.UDID
	securityInfo.ManagementStatus.DeviceUDID = device.UDID
	securityInfo.SecureBoot.DeviceUDID = device.UDID
	securityInfo.SecureBoot.SecureBootReducedSecurity.DeviceUDID = device.UDID

	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Saving SecurityInfo"})
	err := db.DB.Session(&gorm.Session{FullSaveAssociations: true}).Model(&securityInfo).Updates(&securityInfo).Error
	if err != nil {
		return errors.Wrap(err, "Update SecurityInfo Association")
	}

	err = db.DB.Session(&gorm.Session{FullSaveAssociations: true}).Model(&securityInfo.FirmwarePasswordStatus).Updates(&securityInfo.FirmwarePasswordStatus).Error
	if err != nil {
		return errors.Wrap(err, "Update FirmwarePasswordStatus Association")
	}

	err = db.DB.Session(&gorm.Session{FullSaveAssociations: true}).Model(&securityInfo.ManagementStatus).Updates(&securityInfo.ManagementStatus).Error
	if err != nil {
		return errors.Wrap(err, "Update ManagementStatus Association")
	}

	err = db.DB.Session(&gorm.Session{FullSaveAssociations: true}).Model(&securityInfo.FirewallSettings).Updates(&securityInfo.FirewallSettings).Error
	if err != nil {
		return errors.Wrap(err, "Update FirewallSettings Association")
	}

	err = db.DB.Session(&gorm.Session{FullSaveAssociations: true}).Model(&securityInfo.SecureBoot).Updates(&securityInfo.SecureBoot).Error
	if err != nil {
		return errors.Wrap(err, "Update SecureBoot Association")
	}

	err = db.DB.Session(&gorm.Session{FullSaveAssociations: true}).Model(&securityInfo.SecureBoot.SecureBootReducedSecurity).Updates(&securityInfo.SecureBoot.SecureBootReducedSecurity).Error
	if err != nil {
		return errors.Wrap(err, "Update SecureBootReducedSecurity Association")
	}

	err = device.UpdateLastSecurityInfo()
	if err != nil {
		return errors.Wrap(err, "Update LastSecurityInfo")
	}

	return nil
}
