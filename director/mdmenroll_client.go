package director

import (
	"encoding/base64"
	"io"
	"net/http"
	"time"

	"github.com/groob/plist"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
)

// machineInfoPlist represents the MachineInfo plist
type machineInfoPlist struct {
	IMEI                       string `plist:"IMEI,omitempty"`
	MEID                       string `plist:"MEID,omitempty"`
	OSVersion                  string `plist:"OS_VERSION,omitempty"`
	Product                    string `plist:"PRODUCT"`
	Serial                     string `plist:"SERIAL"`
	SupplementalBuildVersion   string `plist:"SUPPLEMENTAL_BUILD_VERSION,omitempty"`
	SupplementalOSVersionExtra string `plist:"SUPPLEMENTAL_OS_VERSION_EXTRA,omitempty"`
	UDID                       string `plist:"UDID"`
	Version                    string `plist:"VERSION,omitempty"`
}

// buildMachineInfoHeader constructs machineInfo plist from device's attributes
func buildMachineInfoHeader(device types.Device) (string, error) {
	info := machineInfoPlist{
		IMEI:                       device.IMEI,
		MEID:                       device.MEID,
		OSVersion:                  device.OSVersion,
		Product:                    device.Model,
		Serial:                     device.SerialNumber,
		SupplementalBuildVersion:   device.SupplementalBuildVersion,
		SupplementalOSVersionExtra: device.SupplementalOSVersionExtra,
		UDID:                       device.UDID,
		Version:                    device.BuildVersion,
	}

	plistBytes, err := plist.Marshal(info)
	if err != nil {
		return "", errors.Wrap(err, "marshal MachineInfo plist")
	}

	return base64.StdEncoding.EncodeToString(plistBytes), nil
}

// fetchEnrollmentProfileFromMDMEnroll gets enrollment profile from MDMEnroll's re-enroll endpoint
func fetchEnrollmentProfileFromMDMEnroll(device types.Device) ([]byte, error) {
	url := utils.MDMEnrollURL() + utils.MDMEnrollReEnrollPath()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "create MDMEnroll request")
	}

	req.Header.Set("Authorization", "Bearer "+utils.MDMEnrollAPIToken())

	headerValue, err := buildMachineInfoHeader(device)
	if err != nil {
		return nil, errors.Wrap(err, "build MachineInfo header")
	}
	req.Header.Set("X-Apple-Aspen-Deviceinfo", headerValue)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "MDMEnroll request failed")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read MDMEnroll response body")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("MDMEnroll returned status %d: %s", resp.StatusCode, string(body))
	}

	if len(body) == 0 {
		return nil, errors.New("MDMEnroll returned empty enrollment profile")
	}

	InfoLogger(LogHolder{
		DeviceUDID:   device.UDID,
		DeviceSerial: device.SerialNumber,
		Message:      "Successfully fetched enrollment profile from MDMEnroll",
	})
	return body, nil
}
