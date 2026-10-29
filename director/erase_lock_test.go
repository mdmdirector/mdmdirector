package director

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
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
	if flag.Lookup("prometheus") == nil {
		flag.Bool("prometheus", false, "Enable prometheus metrics")
	}
}

// fastEscrowRetries shrinks the escrow backoff so retry tests don't sleep.
func fastEscrowRetries(t *testing.T) {
	t.Helper()
	orig := escrowRetryConfig
	escrowRetryConfig = utils.RetryConfig{MaxRetries: 3, MaxBackoff: time.Millisecond, BackoffFactor: 1.5}
	t.Cleanup(func() { escrowRetryConfig = orig })
}

// startEscrowServer points the escrowurl flag at handler for the test.
func startEscrowServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	registerEscrowFlag()
	flag.Set("escrowurl", server.URL) //nolint:errcheck
	t.Cleanup(func() {
		flag.Set("escrowurl", "") //nolint:errcheck
		server.Close()
	})
}

func countLogs(hook *test.Hook, level logrus.Level) int {
	n := 0
	for _, entry := range hook.AllEntries() {
		if entry.Level == level {
			n++
		}
	}
	return n
}

func TestEscrowPinRetriesTransientFailure(t *testing.T) {
	fastEscrowRetries(t)
	var requests int32
	var serials []string
	startEscrowServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		serials = append(serials, r.PostForm.Get("serial"))
		assert.Equal(t, "123456", r.PostForm.Get("recovery_password"))
		assert.Equal(t, "unlock_pin", r.PostForm.Get("secret_type"))
		if atomic.AddInt32(&requests, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	hook := test.NewGlobal()
	defer hook.Reset()

	err := escrowPin(types.Device{UDID: "test-udid", SerialNumber: "test-serial"}, "123456")
	assert.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&requests))
	// The form body must be resent intact on every attempt
	assert.Equal(t, []string{"test-serial", "test-serial", "test-serial"}, serials)

	assert.Equal(t, 2, countLogs(hook, logrus.WarnLevel))
	assert.Equal(t, 0, countLogs(hook, logrus.ErrorLevel))
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel {
			assert.Equal(t, "test-udid", entry.Data["device_udid"])
			assert.Equal(t, "test-serial", entry.Data["device_serial"])
			assert.Contains(t, entry.Message, "status 503")
		}
	}
}

func TestEscrowPinGivesUpAfterMaxRetries(t *testing.T) {
	fastEscrowRetries(t)
	var requests int32
	startEscrowServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusInternalServerError)
	})

	hook := test.NewGlobal()
	defer hook.Reset()

	err := escrowPin(types.Device{UDID: "test-udid", SerialNumber: "test-serial"}, "123456")
	assert.Error(t, err)
	assert.Equal(t, int32(4), atomic.LoadInt32(&requests))
	assert.Equal(t, 3, countLogs(hook, logrus.WarnLevel))
	assert.Equal(t, 1, countLogs(hook, logrus.ErrorLevel))
}

func TestEscrowPinDoesNotRetryClientError(t *testing.T) {
	fastEscrowRetries(t)
	var requests int32
	startEscrowServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusBadRequest)
	})

	hook := test.NewGlobal()
	defer hook.Reset()

	err := escrowPin(types.Device{UDID: "test-udid", SerialNumber: "test-serial"}, "123456")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	assert.Equal(t, int32(1), atomic.LoadInt32(&requests))
	assert.Equal(t, 0, countLogs(hook, logrus.WarnLevel))
}

func TestEscrowPinRetriesConnectionError(t *testing.T) {
	fastEscrowRetries(t)
	// A closed server refuses connections
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	registerEscrowFlag()
	flag.Set("escrowurl", server.URL) //nolint:errcheck
	defer flag.Set("escrowurl", "")   //nolint:errcheck

	hook := test.NewGlobal()
	defer hook.Reset()

	err := escrowPin(types.Device{UDID: "test-udid", SerialNumber: "test-serial"}, "123456")
	assert.Error(t, err)
	assert.Equal(t, 3, countLogs(hook, logrus.WarnLevel))
	assert.Equal(t, 1, countLogs(hook, logrus.ErrorLevel))
}

func TestGeneratePinReturnsLookupError(t *testing.T) {
	if flag.Lookup("debug") == nil {
		flag.Bool("debug", false, "")
	}
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(`SELECT \* FROM "unlock_pins"`).
		WillReturnError(errors.New("connection reset by peer"))

	pin, err := generatePin(types.Device{UDID: "test-udid"})
	assert.Error(t, err)
	assert.Empty(t, pin)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
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
	fastEscrowRetries(t)
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
