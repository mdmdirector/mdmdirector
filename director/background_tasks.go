package director

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/types"
	"github.com/grahamgilbert/mdmdirector/utils"
)

func RetryCommands() {
	var delay time.Duration
	if utils.DebugMode() {
		delay = 20
	} else {
		delay = 120
	}
	ticker := time.NewTicker(delay * time.Second)
	defer ticker.Stop()
	fn := func() {
		pushNotNow()
	}

	fn()

	for {
		select {
		case <-ticker.C:
			fn()
		}
	}
}

// func PushAllDevices() {
// 	var delay time.Duration
// 	if utils.DebugMode() {
// 		delay = 1
// 	} else {
// 		delay = 24
// 	}
// 	ticker := time.NewTicker(delay * time.Hour)
// 	defer ticker.Stop()
// 	fn := func() {
// 		pushAll()
// 	}

// 	fn()

// 	for {
// 		select {
// 		case <-ticker.C:
// 			fn()
// 		}
// 	}
// }

// func PushUnack() {
// 	var delay time.Duration
// 	if utils.DebugMode() {
// 		delay = 1
// 	} else {
// 		delay = 60
// 	}
// 	ticker := time.NewTicker(delay * time.Minute)
// 	defer ticker.Stop()
// 	fn := func() {
// 		pushPending()
// 	}

// 	fn()

// 	for {
// 		select {
// 		case <-ticker.C:
// 			fn()
// 		}
// 	}
// }

// func pushPending() {
// 	var command types.Command
// 	var commands []types.Command
// 	err := db.DB.Model(&command).Select("DISTINCT(device_ud_id)").Where("status = ?", "").Scan(&commands).Error
// 	if err != nil {
// 		log.Print(err)
// 	}

// 	client := &http.Client{}

// 	for _, queuedCommand := range commands {

// 		endpoint, err := url.Parse(utils.ServerURL())
// 		endpoint.Path = path.Join(endpoint.Path, "push", queuedCommand.DeviceUDID)
// 		req, err := http.NewRequest("GET", endpoint.String(), nil)
// 		req.SetBasicAuth("micromdm", utils.ApiKey())

// 		resp, err := client.Do(req)
// 		if err != nil {
// 			log.Print(err)
// 			continue
// 		}

// 		resp.Body.Close()
// 	}
// }

func pushNotNow() {
	var command types.Command
	var commands []types.Command
	err := db.DB.Model(&command).Select("DISTINCT(device_ud_id)").Where("status = ?", "NotNow").Scan(&commands).Error
	if err != nil {
		log.Print(err)
	}

	client := &http.Client{}

	for _, queuedCommand := range commands {

		endpoint, err := url.Parse(utils.ServerURL())
		retry := time.Now().Unix() + 3600
		endpoint.Path = path.Join(endpoint.Path, "push", queuedCommand.DeviceUDID)
		queryString := endpoint.Query()
		queryString.Set("expiration", string(strconv.FormatInt(retry, 10)))
		endpoint.RawQuery = queryString.Encode()
		req, err := http.NewRequest("GET", endpoint.String(), nil)
		req.SetBasicAuth("micromdm", utils.ApiKey())

		resp, err := client.Do(req)
		if err != nil {
			log.Print(err)
			continue
		}

		resp.Body.Close()
	}
}

func pushAll() {
	// var device types.Device
	var devices []types.Device
	err := db.DB.Find(&devices).Scan(&devices).Error
	if err != nil {
		log.Print(err)
	}

	client := &http.Client{}
	if utils.DebugMode() {
		log.Print("Pushing to all in debug mode")
	}

	for _, device := range devices {
		go pushConcurrent(device, client)
	}
}

func pushConcurrent(device types.Device, client *http.Client) {
	endpoint, err := url.Parse(utils.ServerURL())
	retry := time.Now().Unix() + 3600
	endpoint.Path = path.Join(endpoint.Path, "push", device.UDID)
	queryString := endpoint.Query()
	queryString.Set("expiration", string(strconv.FormatInt(retry, 10)))
	endpoint.RawQuery = queryString.Encode()
	req, err := http.NewRequest("GET", endpoint.String(), nil)
	req.SetBasicAuth("micromdm", utils.ApiKey())

	resp, err := client.Do(req)
	if err != nil {
		log.Print(err)
	}

	resp.Body.Close()
}

func PushDevice(udid string) {

	client := &http.Client{}

	endpoint, err := url.Parse(utils.ServerURL())
	retry := time.Now().Unix() + 3600

	endpoint.Path = path.Join(endpoint.Path, "push", udid)
	queryString := endpoint.Query()
	queryString.Set("expiration", string(strconv.FormatInt(retry, 10)))
	endpoint.RawQuery = queryString.Encode()
	req, err := http.NewRequest("GET", endpoint.String(), nil)
	req.SetBasicAuth("micromdm", utils.ApiKey())

	resp, err := client.Do(req)
	if err != nil {
		log.Print(err)
	}

	resp.Body.Close()
}

func ScheduledCheckin() {
	// var delay time.Duration
	ticker := time.NewTicker(30 * time.Minute)
	if utils.DebugMode() {
		ticker = time.NewTicker(20 * time.Second)
	}

	defer ticker.Stop()
	fn := func() {
		processScheduledCheckin()
		// clearCommands()
	}

	fn()

	for {
		select {
		case <-ticker.C:
			fn()
		}
	}
}

func processScheduledCheckin() {
	// var devices []types.Device
	// var device types.Device
	var awaitingConfigDevices []types.Device
	var awaitingConfigDevice types.Device

	// twoHoursAgo := time.Now().Add(-1 * time.Hour)

	// if utils.DebugMode() {
	// 	twoHoursAgo = time.Now().Add(-2 * time.Minute)
	// }
	// err := db.DB.Find(&devices).Scan(&devices).Error
	// if err != nil {
	// 	log.Print(err)
	// }

	// for _, staleDevice := range devices {
	// 	PushDevice(staleDevice.UDID)
	// }
	if utils.DebugMode() {
		log.Print("Processing scheduledCheckin in debug mode")
	}
	pushAll()
	thirtySecondsAgo := time.Now().Add(-30 * time.Second)

	err := db.DB.Model(&awaitingConfigDevice).Where("updated_at < ? AND awaiting_configuration = ? AND initial_tasks_run = ?", thirtySecondsAgo, true, false).Scan(&awaitingConfigDevices).Error
	if err != nil {
		log.Print(err)
	}

	if len(awaitingConfigDevices) == 0 {
		return
	}

	for _, unconfiguredDevice := range awaitingConfigDevices {
		log.Print("Running initial tasks due to schedule")
		RunInitialTasks(unconfiguredDevice.UDID)
	}
}

func FetchDevicesFromMDM() {

	var deviceModel types.Device
	var devices types.DevicesFromMDM

	client := &http.Client{}
	endpoint, err := url.Parse(utils.ServerURL())
	endpoint.Path = path.Join(endpoint.Path, "v1", "devices")

	req, err := http.NewRequest("POST", endpoint.String(), bytes.NewBufferString("{}"))
	req.SetBasicAuth("micromdm", utils.ApiKey())
	// log.Print(endpoint.String())
	resp, err := client.Do(req)
	if err != nil {
		log.Print(err)
	}

	defer resp.Body.Close()

	responseData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Print(err)
	}

	err = json.Unmarshal(responseData, &devices)
	if err != nil {
		log.Print(err)
	}

	for _, newDevice := range devices.Devices {
		var device types.Device
		device.UDID = newDevice.UDID
		device.SerialNumber = newDevice.SerialNumber
		device.Active = newDevice.EnrollmentStatus
		if newDevice.EnrollmentStatus == true {
			device.AuthenticateRecieved = true
			device.TokenUpdateRecieved = true
			device.InitialTasksRun = true
		}
		err := db.DB.Model(&deviceModel).Where("ud_id = ?", newDevice.UDID).FirstOrCreate(&device).Error
		if err != nil {
			log.Print(err)
		}

	}

}

func clearCommands() {
	var command types.Command
	var commands []types.Command
	clearTime := time.Now().Add(-2 * time.Hour)

	if utils.DebugMode() {
		clearTime = time.Now().Add(-5 * time.Minute)
	}

	err := db.DB.Model(&command).Where("updated_at < ?", clearTime).Where("status = ? OR status = ?", "", "NotNow").Delete(&commands).Error
	if err != nil {
		log.Print(err)
	}
}
