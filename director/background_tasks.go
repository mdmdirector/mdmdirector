package director

import (
	"bytes"
	"encoding/json"
	"io"
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
	err := db.DB.Model(&command).
		Select("DISTINCT(device_ud_id)").
		Where("status = ?", "NotNow").
		Scan(&commands).
		Error
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

	err := db.DB.Model(&awaitingConfigDevice).
		Where("awaiting_configuration = ?", true).
		Scan(&awaitingConfigDevices).
		Error
	if err != nil {
		return errors.Wrap(err, "processUnconfiguredDevices: Scan awaiting config devices")
	}

	for i := range awaitingConfigDevices {
		unconfiguredDevice := awaitingConfigDevices[i]
		DebugLogger(
			LogHolder{
				Message:      "Running initial tasks due to schedule",
				DeviceUDID:   unconfiguredDevice.UDID,
				DeviceSerial: unconfiguredDevice.SerialNumber,
			},
		)
		err := RunInitialTasks(unconfiguredDevice.UDID)
		if err != nil {
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

	responseData, err := io.ReadAll(resp.Body)
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
		err := db.DB.Model(&deviceModel).
			Where("ud_id = ?", newDevice.UDID).
			FirstOrCreate(&device).
			Error
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}

	}
	DevicesFetchedFromMDM = true
	log.Info("Finished fetching devices from MicroMDM...")
}
