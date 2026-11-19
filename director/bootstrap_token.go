package director

import (
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
)

// bootstrapTokenKeeper is the part of the NanoMDM client used here; a var so tests can swap it.
type bootstrapTokenKeeper interface {
	KeepBootstrapToken(udid string) error
	ForgetBootstrapTokenKeep(udid string) error
}

//nolint:gochecknoglobals
var bootstrapTokenKeeperClient = func() (bootstrapTokenKeeper, error) {
	return mdm.Client()
}

// keepBootstrapToken asks NanoMDM to keep device's Bootstrap Token across the
// Authenticate that the enrollment-profile reinstall is about to cause. It is best
// effort: a failure is logged and counted, and the reinstall goes ahead, which loses
// the token exactly as it did before this existed. It reports whether a mark was set.
func keepBootstrapToken(device types.Device) bool {
	if utils.MDMServerType() != string(mdm.ServerTypeNanoMDM) {
		return false
	}
	client, err := bootstrapTokenKeeperClient()
	if err != nil {
		metrics.BootstrapTokenKeep("error").Inc()
		ErrorLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      "Bootstrap token keep: no NanoMDM client: " + err.Error(),
		})
		return false
	}

	err = client.KeepBootstrapToken(device.UDID)
	switch {
	case err == nil:
		metrics.BootstrapTokenKeep("marked").Inc()
		InfoLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      "Bootstrap token keep: marked before enrollment profile reinstall",
		})
		return true
	case errors.Is(err, mdm.ErrBootstrapTokenKeepUnknownDevice):
		metrics.BootstrapTokenKeep("unknown_device").Inc()
	default:
		metrics.BootstrapTokenKeep("error").Inc()
	}
	ErrorLogger(LogHolder{
		DeviceUDID:   device.UDID,
		DeviceSerial: device.SerialNumber,
		Message:      "Bootstrap token keep: could not mark, the reinstall may clear the token: " + err.Error(),
	})
	return false
}

// forgetBootstrapTokenKeep removes the mark when the reinstall was not sent, so a
// later genuine re-enrollment can't inherit it. NanoMDM also expires it on its own.
func forgetBootstrapTokenKeep(device types.Device) {
	client, err := bootstrapTokenKeeperClient()
	if err == nil {
		err = client.ForgetBootstrapTokenKeep(device.UDID)
	}
	if err != nil {
		ErrorLogger(LogHolder{
			DeviceUDID:   device.UDID,
			DeviceSerial: device.SerialNumber,
			Message:      "Bootstrap token keep: could not remove mark after a failed reinstall: " + err.Error(),
		})
	}
}
