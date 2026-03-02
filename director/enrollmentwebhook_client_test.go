package director

import (
	"encoding/base64"
	"flag"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdmdirector/mdmdirector/types"
	"github.com/micromdm/go4/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registerEnrollWebhookFlags registers the flags used by the enrollment profile webhook client
func registerEnrollWebhookFlags() {
	if flag.Lookup("enroll-webhook-url") == nil {
		var enrollWebhookURL string
		flag.StringVar(&enrollWebhookURL, "enroll-webhook-url", env.String("ENROLL_WEBHOOK_URL", ""), "Full URL of the enrollment profile webhook endpoint.")
	}
	if flag.Lookup("enroll-webhook-token") == nil {
		var enrollWebhookToken string
		flag.StringVar(&enrollWebhookToken, "enroll-webhook-token", env.String("ENROLL_WEBHOOK_TOKEN", ""), "Bearer token for the enrollment profile webhook.")
	}
}

// setFlagValue sets the value of a registered flag by name
func setFlagValue(t *testing.T, name, value string) {
	t.Helper()
	err := flag.Set(name, value)
	require.NoError(t, err, "failed to set flag %q", name)
}

func TestFetchEnrollmentProfileFromWebhook_Success(t *testing.T) {
	registerEnrollWebhookFlags()

	expectedProfile := []byte("<?xml version=\"1.0\"?><plist><dict></dict></plist>")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Authorization header
		assert.Equal(t, "Bearer test-token-123", r.Header.Get("Authorization"))

		// Verify device info header is present and valid base64
		deviceInfoHeader := r.Header.Get("X-Apple-Aspen-Deviceinfo")
		assert.NotEmpty(t, deviceInfoHeader)
		_, err := base64.StdEncoding.DecodeString(deviceInfoHeader)
		assert.NoError(t, err, "X-Apple-Aspen-Deviceinfo must be valid base64")

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(expectedProfile)
	}))
	defer server.Close()

	setFlagValue(t, "enroll-webhook-url", server.URL)
	setFlagValue(t, "enroll-webhook-token", "test-token-123")

	device := types.Device{
		UDID:         "TEST-UDID-0001",
		SerialNumber: "C02TEST0001",
		Model:        "MacBookPro18,3",
	}

	profile, err := fetchEnrollmentProfileFromWebhook(device)
	require.NoError(t, err)
	assert.Equal(t, expectedProfile, profile)
}

func TestFetchEnrollmentProfileFromWebhook_Non200Status(t *testing.T) {
	registerEnrollWebhookFlags()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	setFlagValue(t, "enroll-webhook-url", server.URL)
	setFlagValue(t, "enroll-webhook-token", "bad-token")

	device := types.Device{UDID: "TEST-UDID-0002", SerialNumber: "C02TEST0002", Model: "iPad13,4"}

	_, err := fetchEnrollmentProfileFromWebhook(device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestFetchEnrollmentProfileFromWebhook_EmptyBody(t *testing.T) {
	registerEnrollWebhookFlags()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write nothing — empty body.
	}))
	defer server.Close()

	setFlagValue(t, "enroll-webhook-url", server.URL)
	setFlagValue(t, "enroll-webhook-token", "some-token")

	device := types.Device{UDID: "TEST-UDID-0003", SerialNumber: "C02TEST0003", Model: "iPad13,4"}

	_, err := fetchEnrollmentProfileFromWebhook(device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty enrollment profile")
}

func TestFetchEnrollmentProfileFromWebhook_DeviceInfoHeaderEncoded(t *testing.T) {
	registerEnrollWebhookFlags()

	device := types.Device{
		UDID:         "DEVICE-UDID-7",
		SerialNumber: "SN007",
		Model:        "iPad13,16",
		OSVersion:    "17.0",
		BuildVersion: "21A329",
	}

	// Pre-compute expected header so we can compare against what the server receives.
	expectedHeader, err := buildMachineInfoHeader(device)
	require.NoError(t, err)

	var receivedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Apple-Aspen-Deviceinfo")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("profile"))
	}))
	defer server.Close()

	setFlagValue(t, "enroll-webhook-url", server.URL)
	setFlagValue(t, "enroll-webhook-token", "tok")

	_, err = fetchEnrollmentProfileFromWebhook(device)
	require.NoError(t, err)
	assert.Equal(t, expectedHeader, receivedHeader)
}
