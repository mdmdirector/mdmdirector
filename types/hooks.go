package types

import (
	"time"

	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/log"
)

func BumpDeviceLastUpdated(udid string) {
	t := time.Now()
	if udid == "" {
		return
	}
	var device Device
	err := db.DB.Model(&device).Where("ud_id = ?", udid).Updates(Device{
		UpdatedAt: t,
	}).Error
	if err != nil {
		log.Error(err)
	}
}
