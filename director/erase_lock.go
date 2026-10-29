package director

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	intErrors "errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/ajg/form.v1"

	"github.com/groob/plist"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"gorm.io/gorm"
)

func EraseLockDevice(udid string) error {

	device, err := GetDevice(udid)
	if err != nil {
		return errors.Wrap(err, "EraseLockDevice:GetDevice")
	}

	var requestType string

	pin, err := generatePin(device)

	if err != nil {
		return errors.Wrap(err, "EraseLockDevice::generatePin")
	}

	if device.Lock {
		requestType = "DeviceLock"
	}

	// Erase wins if both are set
	if device.Erase {
		requestType = "EraseDevice"
	}

	if requestType == "" {
		log.Info("Neither lock or erase are set")
		return nil
	}

	// Inspect the devices queue to see if the command is already there
	deviceQueue, err := InspectCommandQueue(device)
	if err != nil {
		return errors.Wrap(err, "EraseLockDevice:InspectCommandQueue")
	}

	ok, err := checkForExistingCommand(deviceQueue, requestType)
	if err != nil {
		return errors.Wrap(err, "EraseLockDevice:checkForExistingCommand")
	}
	if ok {
		log.Infof("Command %v for %v already in queue, skipping", requestType, device.UDID)
		return nil
	}

	log.Infof("Command %v for %v not found in queue, clearing queue", requestType, device.UDID)
	// Clear the queue for this device
	err = clearCommandQueue(device)
	if err != nil {
		log.Warnf("ClearQueue failed: %v", err.Error())
	}

	err = escrowPin(device, pin)
	if err != nil {
		return errors.Wrap(err, "EraseLockDevice:escrowPin")
	}
	log.Infof("Sending %v to %v", requestType, device.UDID)
	var payload types.CommandPayload
	payload.UDID = device.UDID
	payload.RequestType = requestType
	payload.Pin = pin
	_, err = SendCommand(payload)
	if err != nil {
		return errors.Wrap(err, "EraseLockDevice:SendCommand")
	}

	return nil
}

// escrowTimeout bounds each escrow attempt. EraseLockDevice runs inline in the
// Idle check-in webhook, so the worst case (4 attempts plus backoff, ~25s) has
// to stay short.
var escrowTimeout = 5 * time.Second

// escrowRetryConfig retries network errors, 429 and 5xx. A failed escrow also
// blocks the lock/erase command, so the next Idle check-in tries again.
var escrowRetryConfig = utils.RetryConfig{
	MaxRetries:    3,
	MaxBackoff:    5 * time.Second,
	BackoffFactor: 1.5,
}

func escrowPin(device types.Device, pin string) error {
	escrowURL := utils.EscrowURL()
	if escrowURL == "" {
		DebugLogger(
			LogHolder{
				DeviceUDID:   device.UDID,
				DeviceSerial: device.SerialNumber,
				Message:      "No Escrow URL set, returning early",
			},
		)
		return nil
	}
	endpoint, err := url.Parse(escrowURL)
	if err != nil {
		return errors.Wrap(err, "escrowPin:URL")
	}

	urlString := strings.Trim(endpoint.String(), "/")
	urlString += "/"

	var payload types.EscrowPayload

	payload.Serial = device.SerialNumber
	payload.Pin = pin
	payload.Username = "mdmdirector"
	payload.SecretType = "unlock_pin"

	// log.Debug(payload)

	encoded, err := form.EncodeToValues(payload)
	if err != nil {
		return errors.Wrap(err, "escrowPin")
	}

	req, err := http.NewRequest(http.MethodPost, urlString, strings.NewReader(encoded.Encode()))
	if err != nil {
		return errors.Wrap(err, "escrowPin:NewRequest")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	retryConfig := escrowRetryConfig
	retryConfig.OnRetry = func(attempt int, wait time.Duration, err error, statusCode int) {
		reason := fmt.Sprintf("status %v", statusCode)
		if err != nil {
			reason = err.Error()
		}
		WarnLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      fmt.Sprintf("Failed to escrow pin on attempt %d of %d (%v), retrying in %v", attempt, retryConfig.MaxRetries+1, reason, wait),
		})
		if utils.Prometheus() {
			metrics.PinEscrow("retry").Inc()
		}
	}
	client := utils.NewHTTPClient(escrowTimeout, &retryConfig)

	log.Debugf("Escrowing %v to %v", device.UDID, urlString)
	response, err := client.Do(req)
	if err != nil {
		if response != nil {
			response.Body.Close()
		}
		ErrorLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      fmt.Sprintf("Failed to escrow pin: %v", err),
		})
		if utils.Prometheus() {
			metrics.PinEscrow("error").Inc()
		}
		return errors.Wrap(err, "escrowPin")
	}

	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)

	if err != nil {
		ErrorLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      fmt.Sprintf("Failed to read escrow response: %v", err),
		})
		if utils.Prometheus() {
			metrics.PinEscrow("error").Inc()
		}
		return errors.Wrap(err, "escrowPin:ReadBody")
	}

	if response.StatusCode != http.StatusOK {
		ErrorLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      fmt.Sprintf("Failed to escrow pin, status %v: %v", response.StatusCode, string(body)),
		})
		if utils.Prometheus() {
			metrics.PinEscrow("error").Inc()
		}
		return errors.Errorf("escrowPin: server returned %v: %v", response.StatusCode, string(body))
	}

	InfoLogger(LogHolder{
		DeviceUDID:   device.UDID,
		DeviceSerial: device.SerialNumber,
		Message:      "Successfully escrowed pin",
	})

	if utils.Prometheus() {
		metrics.PinEscrow("success").Inc()
	}
	return nil
}

func generatePin(device types.Device) (string, error) {
	// Look for an existing unlock pin generated within the last 30 mins
	var savedUnlockPin types.UnlockPin

	if device.UnlockPin != "" {
		return device.UnlockPin, nil
	}
	thirtyMinsAgo := time.Now().Add(-30 * time.Minute)

	if utils.DebugMode() {
		thirtyMinsAgo = time.Now().Add(-5 * time.Minute)
	}

	if err := db.DB.Where("unlock_pins.pin_set > ? AND unlock_pins.device_ud_id = ?", thirtyMinsAgo, device.UDID).Order("pin_set DESC").First(&savedUnlockPin).Error; err != nil {
		if intErrors.Is(err, gorm.ErrRecordNotFound) {
			log.Debug("Pin was created more than 30 mins ago")
			out := ""
			for i := 0; i < 6; i++ {
				result, err := rand.Int(rand.Reader, big.NewInt(10))
				if err != nil {
					return "", errors.Wrap(err, "generatePin")
				}
				out += result.String()
			}
			var newUnlockPin types.UnlockPin
			newUnlockPin.DeviceUDID = device.UDID
			newUnlockPin.PinSet = time.Now()
			newUnlockPin.UnlockPin = out
			err = db.DB.Create(&newUnlockPin).Error
			if err != nil {
				return "", errors.Wrap(err, "generatePin:SavePin")
			}

			return out, nil
		}
		return "", errors.Wrap(err, "generatePin:LookupPin")
	}
	// Found a saved one
	return savedUnlockPin.UnlockPin, nil
}

func checkForExistingCommand(body []byte, requestType string) (bool, error) {

	// We don't care about the command UUID
	type Command struct {
		Payload string `json:"payload"`
	}
	type CommandQueue struct {
		Commands []Command `json:"commands"`
	}
	queue := new(CommandQueue)
	err := json.Unmarshal(body, &queue)
	if err != nil {
		return false, errors.Wrap(err, "EraseLockDevice:DecodeCommandQueue")
	}
	for _, cmd := range queue.Commands {
		// decode the command payload
		decodedBytes, err := base64.StdEncoding.DecodeString(cmd.Payload)
		if err != nil {
			return false, errors.Wrap(err, "EraseLockDevice:DecodeBase64Payload")
		}

		type payload struct {
			Command struct {
				RequestType string `plist:"RequestType"`
			} `plist:"Command"`
		}
		cmdPayload := new(payload)
		err = plist.Unmarshal(decodedBytes, cmdPayload)
		if err != nil {
			return false, errors.Wrap(err, "EraseLockDevice:DecodePayloadPlist")
		}

		// If the command is already in the queue, skip it
		if cmdPayload.Command.RequestType == requestType {
			return true, nil
		}
	}
	return false, nil
}
