package director

import (
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
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
	var securityInfo types.SecurityInfo
	var managementStatus types.ManagementStatus
	var firmwarePasswordStatus types.FirmwarePasswordStatus
	var firewallSettings types.FirewallSettings
	// var firewallSettingsApplications []types.FirewallSettingsApplication
	var secureBoot types.SecureBoot
	var secureBootReducedSecurity types.SecureBootReducedSecurity
	securityInfo = securityInfoData.SecurityInfo
	managementStatus = securityInfo.ManagementStatus
	firmwarePasswordStatus = securityInfo.FirmwarePasswordStatus
	firewallSettings = securityInfo.FirewallSettings
	// firewallSettingsApplications = firewallSettings.FirewallSettingsApplications
	secureBoot = securityInfo.SecureBoot
	// secureBoot.DeviceUDID = device.UDID
	secureBootReducedSecurity = securityInfo.SecureBoot.SecureBootReducedSecurity
	secureBootReducedSecurity.DeviceUDID = device.UDID
	securityInfo.DeviceUDID = device.UDID
	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Saving SecurityInfo"})
	err := db.DB.Model(&device).Association("SecurityInfo").Replace(&securityInfo)
	if err != nil {
		return errors.Wrap(err, "Replace SecurityInfo Association")
	}

	err = db.DB.Model(&securityInfo).Association("FirmwarePasswordStatus").Append(&firmwarePasswordStatus)
	if err != nil {
		return errors.Wrap(err, "Append FirmwarePasswordStatus Association")
	}

	err = db.DB.Model(&securityInfo).Association("ManagementStatus").Append(&managementStatus)
	if err != nil {
		return errors.Wrap(err, "Append ManagementStatus Association")
	}

	err = db.DB.Model(&securityInfo).Association("FirewallSettings").Append(&firewallSettings)
	if err != nil {
		return errors.Wrap(err, "Append FirewallSettings Association")
	}

	// err = db.DB.Unscoped().Model(&firewallSettings).Association("FirewallSettingsApplications").Replace(firewallSettingsApplications)
	// if err != nil {
	// 	ErrorLogger(LogHolder{Message: err.Error()})
	// }

	err = db.DB.Model(&securityInfo).Association("SecureBoot").Append(&secureBoot)
	if err != nil {
		return errors.Wrap(err, "Append SecureBoot Association")
	}

	err = db.DB.Model(&secureBoot).Association("SecureBootReducedSecurity").Append(&secureBootReducedSecurity)
	if err != nil {
		return errors.Wrap(err, "Append SecureBootReducedSecurity Association")
	}

	siErr := device.UpdateLastSecurityInfo()
	if siErr != nil {
		ErrorLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: siErr.Error()})
	}

	return nil
}
