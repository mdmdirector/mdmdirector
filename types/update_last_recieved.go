package types

import (
	"time"

	"github.com/mdmdirector/mdmdirector/db"
)

func (device *Device) UpdateLastProfileList() error {
	now := time.Now()
	var deviceModel Device
	err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Updates(Device{LastProfileList: now}).Error
	if err != nil {
		return err
	}
	return nil
}

func (device *Device) UpdateLastCertificateList() error {
	now := time.Now()
	var deviceModel Device
	err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Updates(Device{LastCertificateList: now}).Error
	if err != nil {
		return err
	}
	return nil
}

func (device *Device) UpdateLastDeviceInfo() error {
	now := time.Now()
	var deviceModel Device
	err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Updates(Device{LastDeviceInfo: now}).Error
	if err != nil {
		return err
	}
	return nil
}

func (device *Device) UpdateLastSecurityInfo() error {
	now := time.Now()
	var deviceModel Device
	err := db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Updates(Device{LastSecurityInfo: now}).Error
	if err != nil {
		return err
	}
	return nil
}
