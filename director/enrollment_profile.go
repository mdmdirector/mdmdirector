package director

import (
	"encoding/base64"
	"io/ioutil"

	"github.com/groob/plist"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
)

func ReinstallEnrollmentProfile(device types.Device) error {
	enrollmentProfile := utils.EnrollmentProfile()
	data, err := ioutil.ReadFile(enrollmentProfile)
	if err != nil {
		return errors.Wrap(err, "Failed to read enrollment profile")
	}

	var profile types.DeviceProfile

	err = plist.Unmarshal(data, &profile)
	if err != nil {
		return errors.Wrap(err, "Failed to unmarshal enrollment profile to struct")
	}

	profile.MobileconfigData = data

	InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Pushing new enrollment profile"})

	if utils.SignedEnrollmentProfile() {
		DebugLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Enrollment Profile pre-signed"})
		var commandPayload types.CommandPayload
		commandPayload.RequestType = "InstallProfile"
		commandPayload.Payload = base64.StdEncoding.EncodeToString(profile.MobileconfigData)
		commandPayload.UDID = device.UDID

		_, err := SendCommand(commandPayload)
		if err != nil {
			return errors.Wrap(err, "Failed to push enrollment profile")
		}
	} else {
		DebugLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Signing Enrollment Profile"})
		_, err = PushProfiles([]types.Device{device}, []types.DeviceProfile{profile})
		if err != nil {
			return errors.Wrap(err, "Failed to push enrollment profile")
		}
	}
	return nil
}
