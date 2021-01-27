package director

import (
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
)

func TestInfoLogger(t *testing.T) {

	hook := test.NewGlobal()
	InfoLogger(LogHolder{
		DeviceUDID:         "a_device_udid",
		DeviceSerial:       "a_device_serial",
		CommandUUID:        "a_command_uuid",
		CommandRequestType: "a_command_request_type",
		ProfileUUID:        "a_profile_uuid",
		ProfileIdentifier:  "a_profile_identifier",
		Message:            "this is a message",
		Metric:             "a_metric",
	})

	assert.Equal(t, 1, len(hook.Entries))
	assert.Equal(t, logrus.InfoLevel, hook.LastEntry().Level)
	assert.Equal(t, "this is a message", hook.LastEntry().Message)
	assert.Equal(t, logrus.Fields{
		"command_request_type": "a_command_request_type",
		"command_uuid":         "a_command_uuid",
		"device_serial":        "a_device_serial",
		"device_udid":          "a_device_udid",
		"metric":               "a_metric",
		"profile_identifier":   "a_profile_identifier",
		"profile_uuid":         "a_profile_uuid",
	}, hook.LastEntry().Data)
}
