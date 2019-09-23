package director

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/log"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
)

const MAX = 5
const DelaySeconds = 3600

var DevicesFetchedFromMDM bool

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

func pushNotNow() error {
	var command types.Command
	var commands []types.Command
	err := db.DB.Model(&command).Select("DISTINCT(device_ud_id)").Where("status = ?", "NotNow").Scan(&commands).Error
	if err != nil {
		return err
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
			log.Error(err)
			continue
		}

		resp.Body.Close()
	}
	return nil
}

func shuffleDevices(vals []types.Device) []types.Device {
	r := rand.New(rand.NewSource(time.Now().Unix()))
	ret := make([]types.Device, len(vals))
	perm := r.Perm(len(vals))
	for i, randIndex := range perm {
		ret[i] = vals[randIndex]
	}
	return ret
}

func pushAll() error {
	var devices []types.Device

	err := db.DB.Find(&devices).Scan(&devices).Error
	if err != nil {
		return err
	}

	client := &http.Client{}

	log.Debug("Pushing to all in debug mode")
	sem := make(chan int, MAX)
	counter := 0
	total := 0
	devicesPerSecond := len(devices) / (DelaySeconds - 1)
	for _, device := range shuffleDevices(devices) {
		if counter >= devicesPerSecond {
			log.Infof("Sleeping due to having processed %v devices out of %v. Processing %v per second.", total, len(devices), devicesPerSecond)
			time.Sleep(1 * time.Second)
			counter = 0
		}
		log.Debug("Processed ", counter)
		sem <- 1 // will block if there is MAX ints in sem
		go func() {
			pushConcurrent(device, client)
			<-sem // removes an int from sem, allowing another to proceed
		}()
		counter = counter + 1
		total = total + 1
	}
	log.Infof("Completed pushing to %v devices", counter)
	return nil
}

func pushConcurrent(device types.Device, client *http.Client) {
	// We may re-enable this, but right now let's just push every hour and expire it after two
	// now := time.Now()
	// threeHoursAgo := time.Now().Add(-3 * time.Hour)
	// // If it's been updated within the last three hours, try to push again as it might still be online
	// if device.LastCheckedIn.After(threeHoursAgo) {
	// 	log.Infof("%v checked in within three hours", device.UDID)
	// 	// If it's not been in touch within hour, only push if it's out of date
	// 	if now.Before(device.NextPush) {
	// 		log.Infof("Not pushing to %v, next push is %v", device.UDID, device.NextPush)
	// 		return
	// 	}
	// }
	log.Infof("Pushing to %v", device.UDID)
	endpoint, err := url.Parse(utils.ServerURL())
	retry := time.Now().Unix() + 7200
	endpoint.Path = path.Join(endpoint.Path, "push", device.UDID)
	queryString := endpoint.Query()
	queryString.Set("expiration", string(strconv.FormatInt(retry, 10)))
	endpoint.RawQuery = queryString.Encode()
	req, err := http.NewRequest("GET", endpoint.String(), nil)
	req.SetBasicAuth("micromdm", utils.ApiKey())

	resp, err := client.Do(req)
	if err != nil {
		log.Error(err)
	}

	resp.Body.Close()
}

func PushDevice(udid string) error {

	client := &http.Client{}

	endpoint, err := url.Parse(utils.ServerURL())
	if err != nil {
		return errors.Wrap(err, "PushDevice")
	}

	retry := time.Now().Unix() + 3600

	endpoint.Path = path.Join(endpoint.Path, "push", udid)
	queryString := endpoint.Query()
	queryString.Set("expiration", string(strconv.FormatInt(retry, 10)))
	endpoint.RawQuery = queryString.Encode()
	req, err := http.NewRequest("GET", endpoint.String(), nil)
	if err != nil {
		return errors.Wrap(err, "PushDevice")
	}
	req.SetBasicAuth("micromdm", utils.ApiKey())

	resp, err := client.Do(req)
	if err != nil {
		return errors.Wrap(err, "PushDevice")
	}

	err = resp.Body.Close()
	if err != nil {
		return errors.Wrap(err, "PushDevice")
	}

	return nil
}

func UnconfiguredDevices() {
	ticker := time.NewTicker(30 * time.Second)

	defer ticker.Stop()
	fn := func() {
		err := processUnconfiguredDevices()
		if err != nil {
			log.Error(err)
		}
	}

	fn()

	for {
		select {
		case <-ticker.C:
			fn()
		}
	}
}

func processUnconfiguredDevices() error {

	var awaitingConfigDevices []types.Device
	var awaitingConfigDevice types.Device

	// thirtySecondsAgo := time.Now().Add(-30 * time.Second)

	err := db.DB.Model(&awaitingConfigDevice).Where("awaiting_configuration = ?", true).Scan(&awaitingConfigDevices).Error
	if err != nil {
		return err
	}

	// if len(awaitingConfigDevices) == 0 {
	// 	log.Debug("No unconfigured devices")
	// 	return nil
	// }

	for _, unconfiguredDevice := range awaitingConfigDevices {
		log.Debugf("Running initial tasks due to schedule %v", unconfiguredDevice.UDID)
		err := RunInitialTasks(unconfiguredDevice.UDID)
		if err != nil {
			log.Error(err)
		}
	}

	return nil

}

func ScheduledCheckin() {
	// var delay time.Duration
	ticker := time.NewTicker(DelaySeconds * time.Second)
	if utils.DebugMode() {
		ticker = time.NewTicker(20 * time.Second)
	}

	for {
		if DevicesFetchedFromMDM == false {
			time.Sleep(30 * time.Second)
			log.Info("Devices are still being fetched from MicroMDM")
		} else {
			break
		}
	}

	defer ticker.Stop()
	fn := func() {
		err := processScheduledCheckin()
		if err != nil {
			log.Error(err)
		}
	}

	fn()

	for {
		select {
		case <-ticker.C:
			fn()
		}
	}
}

func processScheduledCheckin() error {

	if utils.DebugMode() {
		log.Debug("Processing scheduledCheckin in debug mode")
	}

	err := pushAll()
	if err != nil {
		return err
	}

	var certificates []types.Certificate

	err = db.DB.Unscoped().Model(&certificates).Where("device_ud_id is NULL").Delete(&types.Certificate{}).Error
	if err != nil {
		return errors.Wrap(err, "processScheduledCheckin::CleanupNullCertificates")
	}

	return nil
}

func FetchDevicesFromMDM() {

	var deviceModel types.Device
	var devices types.DevicesFromMDM
	log.Info("Fetching devices from MicroMDM...")

	client := &http.Client{}
	endpoint, err := url.Parse(utils.ServerURL())
	endpoint.Path = path.Join(endpoint.Path, "v1", "devices")

	req, err := http.NewRequest("POST", endpoint.String(), bytes.NewBufferString("{}"))
	req.SetBasicAuth("micromdm", utils.ApiKey())
	resp, err := client.Do(req)
	if err != nil {
		log.Error(err)
	}

	if resp.StatusCode != 200 {
		return
	}

	defer resp.Body.Close()

	responseData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error(err)
	}

	err = json.Unmarshal(responseData, &devices)
	if err != nil {
		log.Error(err)
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
		if newDevice.UDID == "" {
			continue
		}
		err := db.DB.Model(&deviceModel).Where("ud_id = ?", newDevice.UDID).FirstOrCreate(&device).Error
		if err != nil {
			log.Error(err)
		}

	}
	DevicesFetchedFromMDM = true
	log.Info("Finished fetching devices from MicroMDM...")
}
