package director

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/mdm"
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

	// Use NanoMDM client if enabled
	if utils.MDMServerType() == string(mdm.ServerTypeNanoMDM) {
		log.Info("Fetching devices from NanoMDM...")

		client, err := mdm.Client()
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			return
		}

		resp, err := client.GetAllEnrollments(nil)
		if err != nil {
			ErrorLogger(LogHolder{Message: errors.Wrap(err, "FetchDevicesFromMDM via NanoMDM").Error()})
			return
		}

		for _, enrollment := range resp.Enrollments {
			if enrollment.ID == "" {
				continue
			}

			var device types.Device
			device.UDID = enrollment.ID
			device.Active = enrollment.Enabled

			if enrollment.Device != nil {
				device.SerialNumber = enrollment.Device.SerialNumber
			}

			if enrollment.Enabled {
				device.AuthenticateRecieved = true
				device.TokenUpdateRecieved = true
				device.InitialTasksRun = true
			}

			err := db.DB.Model(&deviceModel).
				Where("ud_id = ?", enrollment.ID).
				FirstOrCreate(&device).
				Error
			if err != nil {
				ErrorLogger(LogHolder{Message: err.Error()})
			}
		}
		DevicesFetchedFromMDM = true
		log.Info("Finished fetching devices from NanoMDM...")
		return
	}

	// MicroMDM implementation
	var devices types.DevicesFromMDM
	log.Info("Fetching devices from MicroMDM...")

	// Handle Micro having a bad day
	var httpClient = &http.Client{
		Timeout: time.Second * 60,
	}

	endpoint, err := url.Parse(utils.ServerURL())
	if err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
	}
	endpoint.Path = path.Join(endpoint.Path, "v1", "devices")

	req, _ := http.NewRequest("POST", endpoint.String(), bytes.NewBufferString("{}"))
	req.SetBasicAuth("micromdm", utils.APIKey())
	resp, err := httpClient.Do(req)
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
