package types

import (
	"fmt"
	"log"
	"time"

	"github.com/grahamgilbert/mdmdirector/db"
)

func BumpDeviceLastUpdated(udid string) {
	t := time.Now()
	fmt.Println("Setting device lastupdated to", t)
	var device Device
	err := db.DB.Model(&device).Where("ud_id = ?", udid).Updates(Device{
		UpdatedAt: t,
	}).Error
	if err != nil {
		log.Print(err)
	}
}
