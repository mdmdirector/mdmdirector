package director

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"gorm.io/gorm"
)

const MAX = 5

var DevicesFetchedFromMDM bool

// The delay between looping over the background goroutines (sending push notifications etc)
func getDelay() time.Duration {
	DelaySeconds := 7200

	if utils.DebugMode() {
		DelaySeconds = 20
	}

	return time.Duration(DelaySeconds)
}

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
		err := pushNotNow()
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
	}

	fn()

	for range ticker.C {
		fn()
	}
}

func pushNotNow() error {
	var command types.Command
	var commands []types.Command
	err := db.DB.Model(&command).Select("DISTINCT(device_ud_id)").Where("status = ?", "NotNow").Scan(&commands).Error
	if err != nil {
		return errors.Wrap(err, "Select NotNow Devices")
	}

	client := &http.Client{}
	for i := range commands {
		queuedCommand := commands[i]
		endpoint, err := url.Parse(utils.ServerURL())
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
		retry := time.Now().Unix() + 3600
		endpoint.Path = path.Join(endpoint.Path, "push", queuedCommand.DeviceUDID)

		queryString := endpoint.Query()
		queryString.Set("expiration", strconv.FormatInt(retry, 10))
		endpoint.RawQuery = queryString.Encode()
		req, err := http.NewRequest("GET", endpoint.String(), nil)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
		req.SetBasicAuth("micromdm", utils.APIKey())

		resp, err := client.Do(req)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			continue
		}

		resp.Body.Close()
		if utils.Prometheus() {
			TotalPushes.Inc()
		}
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
	var dbDevices []types.Device

	DelaySeconds := getDelay()

	err := db.DB.Find(&dbDevices).Scan(&dbDevices).Error
	if err != nil {
		return errors.Wrap(err, "PushAll: Scan devices")
	}

	for i := range dbDevices {
		dbDevice := dbDevices[i]
		now := time.Now()
		threeHoursAgo := time.Now().Add(-3 * time.Hour)
		// sixHoursAgo := time.Now().Add(-6 * time.Hour)
		oneDayAgo := time.Now().Add(-24 * time.Hour)
		hourAgo := time.Now().Add(-60 * time.Minute)

		InfoLogger(LogHolder{DeviceUDID: dbDevice.UDID, DeviceSerial: dbDevice.SerialNumber, Message: "Considering device for scheduled push"})

		if now.Before(dbDevice.NextPush) && !dbDevice.NextPush.IsZero() {
			InfoLogger(LogHolder{DeviceUDID: dbDevice.UDID, DeviceSerial: dbDevice.SerialNumber, Message: "Not Pushing. Next push is in metric", Metric: dbDevice.NextPush.String()})
			continue
		}

		if dbDevice.LastScheduledPush.After(hourAgo) {
			InfoLogger(LogHolder{DeviceUDID: dbDevice.UDID, DeviceSerial: dbDevice.SerialNumber, Message: "Have pushed within the last hour, not pushing again"})
			continue
		}

		sendPush := sendCommandToLazyMachines(dbDevice)
		if sendPush {
			devices = append(devices, dbDevice)
			continue
		}

		if dbDevice.LastCertificateList.IsZero() || dbDevice.LastProfileList.IsZero() || dbDevice.LastSecurityInfo.IsZero() || dbDevice.LastDeviceInfo.IsZero() {
			InfoLogger(LogHolder{DeviceUDID: dbDevice.UDID, DeviceSerial: dbDevice.SerialNumber, Message: "One or more of the info commands hasn't ever been received"})
			devices = append(devices, dbDevice)
			continue
		}

		// We've not had all of the info payloads within the last day
		if (dbDevice.LastCertificateList.Before(oneDayAgo) || dbDevice.LastProfileList.Before(oneDayAgo) || dbDevice.LastSecurityInfo.Before(oneDayAgo) || dbDevice.LastDeviceInfo.Before(oneDayAgo)) && (!dbDevice.LastCertificateList.IsZero() && !dbDevice.LastProfileList.IsZero() && !dbDevice.LastSecurityInfo.IsZero() && !dbDevice.LastDeviceInfo.IsZero()) {
			InfoLogger(LogHolder{DeviceUDID: dbDevice.UDID, DeviceSerial: dbDevice.SerialNumber, Message: "Have not received all of the info commands within the last six hours."})
			devices = append(devices, dbDevice)
			continue
		}

		// If it's been updated within the last three hours, try to push again as it might still be online
		if dbDevice.LastCheckedIn.After(threeHoursAgo) {
			InfoLogger(LogHolder{DeviceUDID: dbDevice.UDID, DeviceSerial: dbDevice.SerialNumber, Message: "Checked in more than three hours ago"})
			if now.Before(dbDevice.NextPush) {
				InfoLogger(LogHolder{DeviceUDID: dbDevice.UDID, DeviceSerial: dbDevice.SerialNumber, Message: "Not Pushing. Next push is in metric", Metric: dbDevice.NextPush.String()})
				continue
			}
		}

		HalfDelaySeconds := DelaySeconds / 2
		lastCheckinDelay := time.Now().Add(-HalfDelaySeconds * time.Second)
		// This contrived bit of logic is to handle devices that don't have a LastScheduledPush set yet
		if !dbDevice.LastScheduledPush.Before(lastCheckinDelay) {
			msg := fmt.Sprintf("%v last pushed in %v which is within %v seconds", dbDevice.UDID, dbDevice.LastScheduledPush, HalfDelaySeconds)
			InfoLogger(LogHolder{DeviceUDID: dbDevice.UDID, DeviceSerial: dbDevice.SerialNumber, Message: msg, Metric: dbDevice.NextPush.String()})
			continue
		}

		InfoLogger(LogHolder{DeviceUDID: dbDevice.UDID, DeviceSerial: dbDevice.SerialNumber, Message: "Adding Device to push list"})
		devices = append(devices, dbDevice)
	}

	DebugLogger(LogHolder{
		Message: "Pushing to all in debug mode",
	})
	sem := make(chan int, MAX)
	counter := 0
	total := 0
	devicesPerSecond := float64(len(devices)) / float64((DelaySeconds - 1))
	DebugLogger(LogHolder{Message: "Processed devices per 0.5 seconds", Metric: strconv.Itoa(int(devicesPerSecond))})
	var shuffledDevices = shuffleDevices(devices)
	for i := range shuffledDevices {
		device := shuffledDevices[i]
		if float64(counter) >= devicesPerSecond {
			InfoLogger(LogHolder{Message: "Sleeping due to having processed devices", Metric: strconv.Itoa(total)})
			time.Sleep(500 * time.Millisecond)
			counter = 0
		}
		DebugLogger(LogHolder{Message: "pushAll processed", Metric: strconv.Itoa(counter)})
		sem <- 1 // will block if there is MAX ints in sem
		go func() {
			// pushConcurrent(device, client)
			err := AddDeviceToScheduledPushQueue(device)
			if err != nil {
				ErrorLogger(LogHolder{Message: err.Error()})
			}
			<-sem // removes an int from sem, allowing another to proceed
		}()
		counter++
		total++
	}
	InfoLogger(LogHolder{Message: "Completed scheduling pushes", Metric: strconv.Itoa(len(devices))})
	return nil
}

func AddDeviceToScheduledPushQueue(device types.Device) error {
	var scheduledPush types.ScheduledPush
	DelaySeconds := getDelay()
	now := time.Now()
	var retry int64
	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Adding scheduled push"})
	if now.After(device.NextPush) {
		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "After scheduled push. Pushing with an expiry of 24 hours.", Metric: device.NextPush.String()})
		retry = time.Now().Unix() + 86400
	} else {
		retry = time.Now().Unix() + int64(DelaySeconds)
	}

	err := db.DB.Model(&scheduledPush).Where(types.ScheduledPush{DeviceUDID: device.UDID}).FirstOrCreate(&scheduledPush, types.ScheduledPush{DeviceUDID: device.UDID, Expiration: retry}).Error
	if err != nil {
		return errors.Wrap(err, "AddDeviceToScheduledPushQueue::ScheduledPushFirstOrCreate")
	}

	return nil
}

func ProcessScheduledCheckinQueue() {

	ticker := time.NewTicker(1 * time.Second)
	client := &http.Client{}

	defer ticker.Stop()
	fn := func() {
		err := pushConcurrent(client)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
	}

	fn()
	for range ticker.C {
		fn()
	}

}

func pushConcurrent(client *http.Client) error {

	var device types.Device
	var scheduledPush types.ScheduledPush
	var scheduledPushes []types.ScheduledPush
	now := time.Now()

	err := db.DB.Model(&scheduledPush).Where("status = ?", "pending").Limit(10).Scan(&scheduledPushes).Error
	if err != nil {
		return errors.Wrap(err, "pushConcurrent::retrievePendingPushes")
	}

	// Mark the devices we are working on as "in_progress" and then perform the push
	for _, push := range scheduledPushes {
		endpoint, err := url.Parse(utils.ServerURL())
		if err != nil {
			return errors.Wrap(err, "pushConcurrent::ParseServerURL")
		}
		err = db.DB.Model(&scheduledPush).Where("id = ?", push.ID).Update("status", "in_progress").Error
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			continue
		}

		InfoLogger(LogHolder{DeviceUDID: push.DeviceUDID, Message: "Sending Push"})

		endpoint.Path = path.Join(endpoint.Path, "push", push.DeviceUDID)
		queryString := endpoint.Query()
		queryString.Set("expiration", strconv.FormatInt(push.Expiration, 10))
		endpoint.RawQuery = queryString.Encode()
		req, err := http.NewRequest("GET", endpoint.String(), nil)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			continue
		}
		req.SetBasicAuth("micromdm", utils.APIKey())

		resp, err := client.Do(req)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}

		err = db.DB.Model(&scheduledPush).Where("id = ?", push.ID).Delete(&types.ScheduledPush{}).Error
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}

		err = db.DB.Model(&device).Select("last_scheduled_push", "next_push").Where("ud_id = ?", push.DeviceUDID).Updates(types.Device{
			LastScheduledPush: now,
			NextPush:          time.Now().Add(30 * time.Minute),
		}).Error
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}

		resp.Body.Close()
		if utils.Prometheus() {
			TotalPushes.Inc()
		}

	}
	return nil
}

func PushDevice(udid string) error {
	client := &http.Client{}

	endpoint, err := url.Parse(utils.ServerURL())
	if err != nil {
		return errors.Wrap(err, "PushDevice")
	}

	retry := time.Now().Unix() + 3600
	if utils.DebugMode() {
		retry = time.Now().Unix() + 30
	}
	endpoint.Path = path.Join(endpoint.Path, "push", udid)
	queryString := endpoint.Query()
	queryString.Set("expiration", strconv.FormatInt(retry, 10))
	endpoint.RawQuery = queryString.Encode()
	req, err := http.NewRequest("GET", endpoint.String(), nil)
	if err != nil {
		return errors.Wrap(err, "PushDevice")
	}
	req.SetBasicAuth("micromdm", utils.APIKey())

	resp, err := client.Do(req)
	if err != nil {
		return errors.Wrap(err, "PushDevice")
	}

	err = resp.Body.Close()
	if err != nil {
		return errors.Wrap(err, "PushDevice")
	}

	InfoLogger(LogHolder{DeviceUDID: udid, Message: "Sent push to device"})

	return nil
}

func UnconfiguredDevices() {
	ticker := time.NewTicker(30 * time.Second)

	defer ticker.Stop()
	fn := func() {
		err := processUnconfiguredDevices()
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
	}

	fn()
	for range ticker.C {
		fn()
	}
}

func processUnconfiguredDevices() error {
	var awaitingConfigDevices []types.Device
	var awaitingConfigDevice types.Device

	err := db.DB.Model(&awaitingConfigDevice).Where("awaiting_configuration = ?", true).Scan(&awaitingConfigDevices).Error
	if err != nil {
		return errors.Wrap(err, "processUnconfiguredDevices: Scan awaiting config devices")
	}

	for i := range awaitingConfigDevices {
		unconfiguredDevice := awaitingConfigDevices[i]
		DebugLogger(LogHolder{Message: "Running initial tasks due to schedule", DeviceUDID: unconfiguredDevice.UDID, DeviceSerial: unconfiguredDevice.SerialNumber})
		err := RunInitialTasks(unconfiguredDevice.UDID)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
	}

	return nil
}

func ScheduledCheckin() {
	var scheduledPushes []types.ScheduledPush
	InfoLogger(LogHolder{Message: "Clearing Scheduled Pushes"})
	err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Model(&scheduledPushes).Delete(&types.ScheduledPush{}).Error
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
	if !utils.DebugMode() {
		rand.Seed(time.Now().UnixNano())
		randomDelay := rand.Intn(600)
		InfoLogger(LogHolder{Metric: strconv.Itoa(randomDelay), Message: "Waiting before beginning to process scheduled checkins"})
		time.Sleep(time.Duration(randomDelay) * time.Second)
	}
	DelaySeconds := getDelay()
	ticker := time.NewTicker(DelaySeconds * time.Second)
	if utils.DebugMode() {
		ticker = time.NewTicker(20 * time.Second)
	}

	counter := 0
	for {
		if !DevicesFetchedFromMDM {
			time.Sleep(30 * time.Second)
			log.Info("Devices are still being fetched from MicroMDM")
			counter++
			if counter > 10 {
				break
			}
		} else {
			break
		}
	}

	defer ticker.Stop()
	fn := func() {
		log.Infof("Running scheduled checkin (%v second) delay", DelaySeconds)
		err := processScheduledCheckin()
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
	}

	fn()

	for range ticker.C {
		go fn()
	}
}

func processScheduledCheckin() error {
	if utils.DebugMode() {
		DebugLogger(LogHolder{Message: "Processing scheduledCheckin in debug mode"})
	}

	err := pushAll()
	if err != nil {
		return errors.Wrap(err, "processScheduledCheckin::pushAll")
	}

	var certificates []types.Certificate

	err = db.DB.Unscoped().Model(&certificates).Where("device_ud_id is NULL").Delete(&types.Certificate{}).Error
	if err != nil {
		return errors.Wrap(err, "processScheduledCheckin::CleanupNullCertificates")
	}

	var profileLists []types.ProfileList

	err = db.DB.Unscoped().Model(&profileLists).Where("device_ud_id is NULL").Delete(&types.ProfileList{}).Error
	if err != nil {
		return errors.Wrap(err, "processScheduledCheckin::CleanupNullProfileLists")
	}

	// var appItems []types.FirewallSettingsApplication

	// err = db.DB.Unscoped().Model(&appItems).Where("firewall_settings_applications_items is NULL").Delete(&types.FirewallSettingsApplication{}).Error
	// if err != nil {
	// 	return errors.Wrap(err, "processScheduledCheckin::CleanupNullFirewallSettingsApplicationsItem")
	// }

	thirtyMinsAgo := time.Now().Add(-30 * time.Minute)
	err = db.DB.Where("unlock_pins.pin_set < ?", thirtyMinsAgo).Delete(&types.UnlockPin{}).Error
	if err != nil {
		return errors.Wrap(err, "processScheduledCheckin::DeleteRandomUnlockPins")
	}

	var device types.Device
	err = db.DB.Model(&device).Not("unlock_pin = ?", "").Where("erase = ? AND lock = ?", false, false).Update("unlock_pin", "").Error
	if err != nil {
		return errors.Wrap(err, "processScheduledCheckin::ResetFixedPin")
	}

	err = db.DB.Unscoped().Where("device_ud_id is NULL").Delete(&types.ScheduledPush{}).Error
	if err != nil {
		return errors.Wrap(err, "processScheduledCheckin::CleanupNullScheduledPushes")
	}

	return nil
}

func FetchDevicesFromMDM() {
	var deviceModel types.Device
	var devices types.DevicesFromMDM
	log.Info("Fetching devices from MicroMDM...")

	// Handle Micro having a bad day
	var client = &http.Client{
		Timeout: time.Second * 60,
	}

	endpoint, err := url.Parse(utils.ServerURL())
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
	endpoint.Path = path.Join(endpoint.Path, "v1", "devices")

	req, _ := http.NewRequest("POST", endpoint.String(), bytes.NewBufferString("{}"))
	req.SetBasicAuth("micromdm", utils.APIKey())
	resp, err := client.Do(req)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}

	if resp.StatusCode != 200 {
		return
	}

	defer resp.Body.Close()

	responseData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}

	err = json.Unmarshal(responseData, &devices)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}

	for _, newDevice := range devices.Devices {
		var device types.Device
		device.UDID = newDevice.UDID
		device.SerialNumber = newDevice.SerialNumber
		device.Active = newDevice.EnrollmentStatus
		if newDevice.EnrollmentStatus {
			device.AuthenticateRecieved = true
			device.TokenUpdateRecieved = true
			device.InitialTasksRun = true
		}
		if newDevice.UDID == "" {
			continue
		}
		err := db.DB.Model(&deviceModel).Where("ud_id = ?", newDevice.UDID).FirstOrCreate(&device).Error
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}

	}
	DevicesFetchedFromMDM = true
	log.Info("Finished fetching devices from MicroMDM...")
}

func sendCommandToLazyMachines(device types.Device) bool {
	weekAgo := time.Now().Add(-168 * time.Hour)
	// Don't bother on devices we've not heard from for ever
	if device.LastCheckedIn.Before(time.Now().Add(-2160 * time.Hour)) {
		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Device has not checked in for 90 days", Metric: device.LastCheckedIn.String()})

		return false
	}

	if device.LastCertificateList.Before(weekAgo) && !device.LastCertificateList.IsZero() {
		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Last Certificate List is over a week old", Metric: device.LastCertificateList.String()})

		return true
	}

	if device.LastProfileList.Before(weekAgo) && !device.LastProfileList.IsZero() {
		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Last Profile List is over a week old", Metric: device.LastProfileList.String()})

		return true
	}

	if device.LastDeviceInfo.Before(weekAgo) && !device.LastDeviceInfo.IsZero() {
		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Last Device Info is over a week old", Metric: device.LastDeviceInfo.String()})

		return true
	}

	if device.LastSecurityInfo.Before(weekAgo) && !device.LastSecurityInfo.IsZero() {
		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Last Security Info is over a week old", Metric: device.LastSecurityInfo.String()})

		return true
	}

	return false
}
