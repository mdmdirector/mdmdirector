package director

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
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
)

const MAX = 5

var DevicesFetchedFromMDM bool

var client = &http.Client{}

// The delay between looping over the background goroutines (sending push notifications etc)
func getDelay() time.Duration {
	if utils.DebugMode() {
		return 20 * time.Second
	}
	return 2 * time.Hour
}

func RetryCommands() {
	delay := time.Duration(120)
	if utils.DebugMode() {
		delay = time.Duration(20)
	}

	ticker := time.NewTicker(delay * time.Second)
	defer ticker.Stop()

	pushCommands := func() {
		if err := pushNotNow(); err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
	}

	pushCommands()

	for range ticker.C {
		pushCommands()
	}
}

func pushNotNow() error {
	// Select all devices with status "NotNow"
	var commands []types.Command
	err := db.DB.Model(&types.Command{}).Distinct("device_ud_id").Where("status = ?", "NotNow").Find(&commands).Error
	if err != nil {
		return errors.Wrap(err, "Select NotNow Devices")
	}

	client := &http.Client{}

	// Loop through each device and send a push command
	for _, queuedCommand := range commands {
		// Prepare the endpoint URL with the device's UDID and expiration timestamp
		endpoint, err := url.Parse(utils.ServerURL())
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			continue
		}
		retry := time.Now().Unix() + 3600
		endpoint.Path = path.Join(endpoint.Path, "push", queuedCommand.DeviceUDID)
		queryString := endpoint.Query()
		queryString.Set("expiration", strconv.FormatInt(retry, 10))
		endpoint.RawQuery = queryString.Encode()

		// Send request
		req, err := http.NewRequest("GET", endpoint.String(), nil)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			continue
		}
		req.SetBasicAuth("micromdm", utils.APIKey())
		resp, err := client.Do(req)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			continue
		}
		resp.Body.Close()

		// Increment Prometheus counters
		if utils.Prometheus() {
			TotalPushes.Inc()
		}
	}

	return nil
}

func UnconfiguredDevices() {
	process := func() {
		if err := processUnconfiguredDevices(); err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
	}

	// Execute the process function immediately
	process()

	// Schedule the process function to execute every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		process()
	}
}

func processUnconfiguredDevices() error {
	var awaitingConfigDevices []types.Device
	var awaitingConfigDevice types.Device

	err := db.DB.Model(&awaitingConfigDevice).Where("awaiting_configuration = ?", true).Scan(&awaitingConfigDevices).Error
	if err != nil {
		return errors.Wrap(err, "processUnconfiguredDevices: Scan awaiting config devices")
	}

	// Loop through each device and run initial tasks
	for _, unconfiguredDevice := range awaitingConfigDevices {
		DebugLogger(LogHolder{Message: "Running initial tasks due to schedule", DeviceUDID: unconfiguredDevice.UDID, DeviceSerial: unconfiguredDevice.SerialNumber})
		if err := RunInitialTasks(unconfiguredDevice.UDID); err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
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
		return
	}

	err = json.Unmarshal(responseData, &devices)
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		return
	}

	for _, newDevice := range devices.Devices {
		if newDevice.UDID == "" {
			continue
		}
		device := types.Device{
			UDID:         newDevice.UDID,
			SerialNumber: newDevice.SerialNumber,
			Active:       newDevice.EnrollmentStatus,
		}
		if newDevice.EnrollmentStatus {
			device.AuthenticateRecieved = true
			device.TokenUpdateRecieved = true
			device.InitialTasksRun = true
		}
		err := db.DB.Model(&deviceModel).Where("ud_id = ?", newDevice.UDID).FirstOrCreate(&device).Error
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
	}

	DevicesFetchedFromMDM = true
	log.Info("Finished fetching devices from MicroMDM...")
}
