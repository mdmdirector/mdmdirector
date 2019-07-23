package director

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/types"

	// sqlite
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

func UpdateDevice(newDevice types.Device) *types.Device {
	var device types.Device

	if newDevice.UDID == "" {
		return &newDevice
	}

	if err := db.DB.Where("ud_id = ?", newDevice.UDID).First(&device).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			db.DB.Create(&newDevice)
		}
	} else {
		err := db.DB.Model(&device).Where("ud_id = ?", newDevice.UDID).First(&device).Update(&newDevice).Error
		if err != nil {
			log.Print(err)
		}
		if newDevice.InitialTasksRun == false {
			RunInitialTasks(newDevice.UDID)
		}
	}
	err := db.DB.Model(&device).Where("ud_id = ?", newDevice.UDID).Assign(&newDevice).FirstOrCreate(&newDevice).Error
	if err != nil {
		log.Print(err)
	}

	return &newDevice
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

func GetAllDevices() []types.Device {
	// var device types.Device
	var devices []types.Device

	err := db.DB.Find(&devices).Scan(&devices).Error
	if err != nil {
		fmt.Println(err)
		log.Print("Couldn't scan to Device model")
	}
	return devices
}

func DeviceHandler(w http.ResponseWriter, r *http.Request) {
	var devices []types.Device
	devices = GetAllDevices()

	output, err := json.MarshalIndent(&devices, "", "    ")
	if err != nil {
		log.Print(err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	w.Write(output)

}

func RequestDeviceInformation(device types.Device) {
	var requestType = "DeviceInformation"
	inQueue := CommandInQueue(device, requestType)
	if inQueue {
		log.Printf("%v is already in queue for %v", requestType, device.UDID)
		return
	}
	var payload types.CommandPayload
	payload.UDID = device.UDID
	payload.RequestType = requestType
	payload.Queries = types.DeviceInformationQueries
	SendCommand(payload)
}
