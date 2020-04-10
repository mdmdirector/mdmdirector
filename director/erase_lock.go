package director

import (
	"crypto/rand"
	"io/ioutil"
	"math/big"
	"net/http"
	"net/url"
	"strings"

	"gopkg.in/ajg/form.v1"

	"github.com/mdmdirector/mdmdirector/log"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
)

func EraseLockDevice(device *types.Device) error {
	pin, err := generatePin()
	log.Debugf("Pin is %v", pin)
	if err != nil {
		return errors.Wrap(err, "EraseLockDevice::generatePin")
	}

	var requestType string

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
		return errors.Wrap(err, "EraseLockDevice")
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

func escrowPin(device *types.Device, pin string) error {
	endpoint, err := url.Parse(utils.EscrowURL())
	if err != nil {
		return errors.Wrap(err, "EraseLockDevice::URL")
	}

	urlString := strings.Trim(endpoint.String(), "/")
	urlString += "/"

	var payload types.EscrowPayload

	payload.Serial = device.SerialNumber
	payload.Pin = pin
	payload.Username = "mdmdirector"
	payload.SecretType = "unlock_pin"

	log.Debug(payload)

	encoded, err := form.EncodeToValues(payload)
	if err != nil {
		return errors.Wrap(err, "escrowPin")
	}

	log.Debugf("Escrowing %v to %v", encoded, urlString)
	response, err := http.PostForm(urlString, encoded)

	if err != nil {
		return errors.Wrap(err, "escrowPin")
	}

	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)

	if err != nil {
		return errors.Wrap(err, "escrowPin")
	}

	log.Debug(string(body))

	return nil
}

func generatePin() (string, error) {
	out := ""
	for i := 0; i < 6; i++ {
		result, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", errors.Wrap(err, "generatePin")
		}
		out += result.String()
	}

	return out, nil
}
