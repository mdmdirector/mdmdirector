package types

import (
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/log"
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
