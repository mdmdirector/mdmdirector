package director

import (
	"fmt"
	"log"

	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/types"
	"github.com/jinzhu/gorm"

	// sqlite
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

func UpdateDevice(newDevice types.Device) *types.Device {
	var device types.Device
	if newDevice.UDID == "" {
		return &device
	}

	if err := db.DB.Where("ud_id = ?", newDevice.UDID).First(&device).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			fmt.Println("Device doesn't exist, creating")
			db.DB.Create(&newDevice)
		}
	} else {
		fmt.Println("Device exists, updating")
		err := db.DB.Model(&device).Where("ud_id = ?", newDevice.UDID).First(&device).Updates(types.Device{
			DeviceName:   newDevice.DeviceName,
			BuildVersion: newDevice.BuildVersion,
			ModelName:    newDevice.ModelName,
			Model:        newDevice.Model,
			OSVersion:    newDevice.OSVersion,
			ProductName:  newDevice.ProductName,
			SerialNumber: newDevice.SerialNumber,
			Active:       newDevice.Active,
		}).Error
		if err != nil {
			log.Print(err)
		}

		// fmt.Println("Updating", device.UDID)
		// fmt.Println(device)

	}
	// fmt.Println(device)
	// GetDevice(device.UDID)
	return &device
}

func GetDevice(udid string) types.Device {
	var device types.Device

	err := db.DB.Model(device).Where("ud_id = ?", udid).First(&device).Scan(&device).Error
	if err != nil {
		fmt.Println(err)
		log.Print("Couldn't scan to Device model")
	}
	return device
}

func GetDeviceSerial(serial string) types.Device {
	var device types.Device

	err := db.DB.Model(device).Where("serial_number = ?", serial).First(&device).Scan(&device).Error
	if err != nil {
		fmt.Println(err)
		log.Print("Couldn't scan to Device model")
	}
	return device
}
