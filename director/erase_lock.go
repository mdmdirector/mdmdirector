package director

import (
	"crypto/rand"
	"io/ioutil"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/ajg/form.v1"

	"github.com/jinzhu/gorm"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/log"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
)

func EraseLockDevice(udid string) error {

	device, err := GetDevice(udid)
	if err != nil {
		log.Error(err)
	}

	// if !device.AuthenticateRecieved {
	// 	err := errors.New(device.UDID + " is not ready to receive MDM commands")
	// 	return errors.Wrap(err, "EraseLockDevice:AuthenticateRecieved")
	// }

	// if !device.TokenUpdateRecieved {
	// 	err := errors.New(device.UDID + " is not ready to receive MDM commands")
	// 	return errors.Wrap(err, "EraseLockDevice:TokenUpdateRecieved")
	// }

	// if !device.InitialTasksRun {
	// 	err := errors.New(device.UDID + " is not ready to receive MDM commands")
	// 	return errors.Wrap(err, "EraseLockDevice:InitialTasksRun")
	// }

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

	err = escrowPin(device, pin)
	if err != nil {
		return errors.Wrap(err, "EraseLockDevice:escrowPin")
	}
	log.Debugf("Sending %v to %v", requestType, device.UDID)
	var payload types.CommandPayload
	payload.UDID = device.UDID
	payload.RequestType = requestType
	payload.Pin = pin
	command, err := SendCommand(payload)
	if err != nil {
		return errors.Wrap(err, "EraseLockDevice:SendCommand")
	}

	log.Debugf("Sent %v", command.CommandUUID)

	return nil
}

func escrowPin(device types.Device, pin string) error {
	endpoint, err := url.Parse(utils.EscrowURL())
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

	log.Debugf("Escrowing %v to %v", device.UDID, urlString)
	response, err := http.PostForm(urlString, encoded)

	if err != nil {
		return errors.Wrap(err, "escrowPin")
	}

	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)

	if err != nil {
		return errors.Wrap(err, "escrowPin:"+string(body))
	}

	return nil
}

func generatePin(device types.Device) (string, error) {
	// Look for an existing unlock pin generated within the last 30 mins
	var unlockPinModel types.UnlockPin
	var savedUnlockPin types.UnlockPin
	thirtyMinsAgo := time.Now().Add(-30 * time.Minute)

	if utils.DebugMode() {
		thirtyMinsAgo = time.Now().Add(-5 * time.Minute)
	}

	if err := db.DB.Model(&unlockPinModel).Where("unlock_pins.pin_set > ? AND unlock_pins.device_ud_id = ?", thirtyMinsAgo, device.UDID).Order("pin_set DESC").First(&unlockPinModel).Scan(&savedUnlockPin).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
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
	}
	// Found a saved one
	log.Debugf("Using saved Pin for %v", device.UnlockPin)
	return savedUnlockPin.UnlockPin, nil
}
