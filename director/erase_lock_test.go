package director

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdmdirector/mdmdirector/types"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
)

const testBodyDeviceLock = `{
  "commands": [
    {
      "uuid": "7781b53a-6a35-4b2d-940e-fb8e57dadba5",
      "payload": "PD94bWwgdmVyc2lvbj0iMS4wIiBlbmNvZGluZz0iVVRGLTgiPz4KPCFET0NUWVBFIHBsaXN0IFBVQkxJQyAiLS8vQXBwbGUvL0RURCBQTElTVCAxLjAvL0VOIiAiaHR0cDovL3d3dy5hcHBsZS5jb20vRFREcy9Qcm9wZXJ0eUxpc3QtMS4wLmR0ZCI+CjxwbGlzdCB2ZXJzaW9uPSIxLjAiPjxkaWN0PjxrZXk+Q29tbWFuZDwva2V5PjxkaWN0PjxrZXk+UElOPC9rZXk+PHN0cmluZz4xMjM0NTY8L3N0cmluZz48a2V5PlJlcXVlc3RUeXBlPC9rZXk+PHN0cmluZz5EZXZpY2VMb2NrPC9zdHJpbmc+PC9kaWN0PjxrZXk+Q29tbWFuZFVVSUQ8L2tleT48c3RyaW5nPjc3ODFiNTNhLTZhMzUtNGIyZC05NDBlLWZiOGU1N2RhZGJhNTwvc3RyaW5nPjwvZGljdD48L3BsaXN0Pgo="
    }
  ]
}`

func TestCheckForExistingCommands(t *testing.T) {

	ok, err := checkForExistingCommand([]byte(testBodyDeviceLock), "DeviceLock")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !ok {
		t.Errorf("Expected DeviceLock command to be found, but it was not")
	}

	ok, err = checkForExistingCommand([]byte(testBodyDeviceLock), "EraseDevice")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if ok {
		t.Errorf("Expected EraseDevice command to not be found")
	}

}

// registerEscrowFlag ensures the escrowurl flag is registered exactly once.
// The flag is normally registered in main(), which does not run during tests.
func registerEscrowFlag() {
	if flag.Lookup("escrowurl") == nil {
		flag.String("escrowurl", "", "")
	}
}

func TestEscrowPinSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	registerEscrowFlag()
	flag.Set("escrowurl", server.URL) //nolint:errcheck
	defer flag.Set("escrowurl", "")   //nolint:errcheck

	hook := test.NewGlobal()
	defer hook.Reset()

	device := types.Device{UDID: "test-udid", SerialNumber: "test-serial"}

	err := escrowPin(device, "123456")
	assert.NoError(t, err)

	var found bool
	for _, entry := range hook.Entries {
		if entry.Level == logrus.InfoLevel && entry.Message == "Successfully escrowed pin" {
			found = true
			assert.Equal(t, "test-udid", entry.Data["device_udid"])
			assert.Equal(t, "test-serial", entry.Data["device_serial"])
		}
	}
	assert.True(t, found, "expected info log 'Successfully escrowed pin'")
}

func TestEscrowPinServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error")) //nolint:errcheck
	}))
	defer server.Close()

	registerEscrowFlag()
	flag.Set("escrowurl", server.URL) //nolint:errcheck
	defer flag.Set("escrowurl", "")   //nolint:errcheck

	hook := test.NewGlobal()
	defer hook.Reset()

	device := types.Device{UDID: "test-udid", SerialNumber: "test-serial"}

	err := escrowPin(device, "123456")
	assert.Error(t, err)

	var found bool
	for _, entry := range hook.Entries {
		if entry.Level == logrus.ErrorLevel {
			found = true
			assert.Equal(t, "test-udid", entry.Data["device_udid"])
			assert.Equal(t, "test-serial", entry.Data["device_serial"])
			assert.Contains(t, entry.Message, "500")
		}
	}
	assert.True(t, found, "expected error log for server error response")
}

func TestEscrowPinNoURL(t *testing.T) {
	registerEscrowFlag()
	flag.Set("escrowurl", "") //nolint:errcheck

	originalLevel := logrus.GetLevel()
	logrus.SetLevel(logrus.DebugLevel)
	defer logrus.SetLevel(originalLevel)

	hook := test.NewGlobal()
	defer hook.Reset()

	device := types.Device{UDID: "test-udid", SerialNumber: "test-serial"}

	err := escrowPin(device, "123456")
	assert.NoError(t, err)

	var found bool
	for _, entry := range hook.Entries {
		if entry.Level == logrus.DebugLevel && entry.Message == "No Escrow URL set, returning early" {
			found = true
		}
	}
	assert.True(t, found, "expected debug log 'No Escrow URL set, returning early'")
}
