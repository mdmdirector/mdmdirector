package director

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/micromdm/go4/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registerEnrollmentProfileFlags registers all flags consumed by reinstallEnrollmentProfile
func registerEnrollmentProfileFlags(t *testing.T) {
	t.Helper()
	if flag.Lookup("enrollment-profile") == nil {
		flag.String("enrollment-profile", env.String("ENROLLMENT_PROFILE", ""), "Path to enrollment profile.")
	}
	if flag.Lookup("enrollment-profile-signed") == nil {
		flag.Bool("enrollment-profile-signed", env.Bool("ENROLMENT_PROFILE_SIGNED", false), "Is the enrollment profile pre-signed?")
	}
	if flag.Lookup("enable-reenroll-via-webhook") == nil {
		flag.Bool("enable-reenroll-via-webhook", env.Bool("ENABLE_REENROLL_VIA_WEBHOOK", false), "Enable webhook-based re-enrollment.")
	}
	if flag.Lookup("micromdmurl") == nil {
		flag.String("micromdmurl", env.String("MICROMDM_URL", "http://localhost:9000"), "MicroMDM server URL.")
	}
	if flag.Lookup("micromdmapikey") == nil {
		flag.String("micromdmapikey", env.String("MICROMDM_API_KEY", ""), "MicroMDM API key.")
	}
	if flag.Lookup("sign") == nil {
		flag.Bool("sign", env.Bool("SIGN", false), "Sign profiles prior to sending to MicroMDM.")
	}
	if flag.Lookup("prometheus") == nil {
		flag.Bool("prometheus", env.Bool("PROMETHEUS", false), "Enable Prometheus metrics.")
	}
	if flag.Lookup("enroll-webhook-url") == nil {
		flag.String("enroll-webhook-url", env.String("ENROLL_WEBHOOK_URL", ""), "Enrollment profile webhook URL.")
	}
	if flag.Lookup("enroll-webhook-token") == nil {
		flag.String("enroll-webhook-token", env.String("ENROLL_WEBHOOK_TOKEN", ""), "Enrollment profile webhook bearer token.")
	}
}

// setFlag sets an already-registered flag to value
func setFlag(t *testing.T, name, value string) {
	t.Helper()
	require.NoError(t, flag.Set(name, value), "failed to set flag %q", name)
}

// writeTempProfile writes content to a temporary file and returns its path
func writeTempProfile(t *testing.T, content []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "enrollment.mobileconfig")
	require.NoError(t, os.WriteFile(path, content, 0o600))
	return path
}

// TestReinstallEnrollmentProfile_FileNotFound - missing enrollment profile
func TestReinstallEnrollmentProfile_FileNotFound(t *testing.T) {
	registerEnrollmentProfileFlags(t)
	setFlag(t, "enable-reenroll-via-webhook", "false")
	setFlag(t, "enrollment-profile-signed", "false")
	setFlag(t, "enrollment-profile", "/nonexistent/path/to/enrollment.mobileconfig")

	device := types.Device{UDID: "TEST-UDID-FILE-NOT-FOUND", SerialNumber: "C02NOTFOUND"}
	err := reinstallEnrollmentProfile(device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to read enrollment profile")
}

// TestReinstallEnrollmentProfile_InvalidPlist - invalid Plist
func TestReinstallEnrollmentProfile_InvalidPlist(t *testing.T) {
	registerEnrollmentProfileFlags(t)
	setFlag(t, "enable-reenroll-via-webhook", "false")
	setFlag(t, "enrollment-profile-signed", "false")

	profilePath := writeTempProfile(t, []byte("this is not a valid plist %%%"))
	setFlag(t, "enrollment-profile", profilePath)

	device := types.Device{UDID: "TEST-UDID-BAD-PLIST", SerialNumber: "C02BADPLIST"}
	err := reinstallEnrollmentProfile(device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to unmarshal enrollment profile to struct")
}

// TestReinstallEnrollmentProfile_WebhookError - webhook server error
func TestReinstallEnrollmentProfile_WebhookError(t *testing.T) {
	registerEnrollmentProfileFlags(t)
	setFlag(t, "enable-reenroll-via-webhook", "true")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	setFlag(t, "enroll-webhook-url", server.URL)
	setFlag(t, "enroll-webhook-token", "test-token")

	device := types.Device{UDID: "TEST-UDID-WEBHOOK-ERR", SerialNumber: "C02WEBHOOKERR"}
	err := reinstallEnrollmentProfile(device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to fetch enrollment profile from webhook")
}

// TestReinstallEnrollmentProfile_WebhookEmptyBody - empty profile from webhook
func TestReinstallEnrollmentProfile_WebhookEmptyBody(t *testing.T) {
	registerEnrollmentProfileFlags(t)
	setFlag(t, "enable-reenroll-via-webhook", "true")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// intentionally write no body
	}))
	defer server.Close()

	setFlag(t, "enroll-webhook-url", server.URL)
	setFlag(t, "enroll-webhook-token", "test-token")

	device := types.Device{UDID: "TEST-UDID-WEBHOOK-EMPTY", SerialNumber: "C02WEBHOOKEMPTY"}
	err := reinstallEnrollmentProfile(device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to fetch enrollment profile from webhook")
}

// mockGetDevice sets up DB expectations for GetDevice
func mockGetDevice(mockSpy sqlmock.Sqlmock, udid string) {
	deviceRows1 := sqlmock.NewRows([]string{"ud_id", "serial_number"}).
		AddRow(udid, "C02TEST123")
	deviceRows2 := sqlmock.NewRows([]string{"ud_id", "serial_number"}).
		AddRow(udid, "C02TEST123")

	// First query from First()
	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1 ORDER BY "devices"\."ud_id" LIMIT 1`).
		WithArgs(udid).
		WillReturnRows(deviceRows1)

	// Second query from Scan() - includes primary key in WHERE clause
	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1 AND "devices"\."ud_id" = \$2 ORDER BY "devices"\."ud_id" LIMIT 1`).
		WithArgs(udid, udid).
		WillReturnRows(deviceRows2)
}

// fakeMicroMDMServer returns an httptest.Server that mimics the MicroMDM - /v1/commands
func fakeMicroMDMServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.CommandResponse{}
		resp.Payload.CommandUUID = "test-cmd-uuid-0001"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// minimalUnsignedProfile - valid mobileconfig plist that can be unmarshalled into types.DeviceProfile
const minimalUnsignedProfile = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array/>
	<key>PayloadDisplayName</key>
	<string>Test Enrollment</string>
	<key>PayloadIdentifier</key>
	<string>com.example.test.enrollment</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>AABBCCDD-0001-0002-0003-000000000001</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

// TestReinstallEnrollmentProfile_Webhook_Success verifies the success path where
// the webhook returns a valid profile and PushProfiles sends it successfully
func TestReinstallEnrollmentProfile_Webhook_Success(t *testing.T) {
	registerEnrollmentProfileFlags(t)
	setFlag(t, "enable-reenroll-via-webhook", "true")
	setFlag(t, "enrollment-profile-signed", "false")
	setFlag(t, "sign", "false")

	// Webhook server returns a valid unsigned plist.
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(minimalUnsignedProfile))
	}))
	defer webhookServer.Close()
	setFlag(t, "enroll-webhook-url", webhookServer.URL)
	setFlag(t, "enroll-webhook-token", "test-token")

	// Fake MicroMDM server that PushProfiles/SendCommand will POST to.
	mdmServer := fakeMicroMDMServer(t)
	defer mdmServer.Close()
	setFlag(t, "micromdmurl", mdmServer.URL)

	device := types.Device{UDID: "TEST-UDID-WEBHOOK-OK", SerialNumber: "C02WEBHOOKOK"}
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	mockGetDevice(mockSpy, device.UDID)

	err := reinstallEnrollmentProfile(device)
	require.NoError(t, err)
}

// TestGetEnrollmentProfile_Found - returns enrollment profile from list of profiles
func TestGetEnrollmentProfile_Found(t *testing.T) {
	profileLists := []types.ProfileList{
		{
			PayloadUUID:       "UUID-0001",
			PayloadIdentifier: "com.example.other",
			PayloadContent: []types.PayloadContentItem{
				{PayloadType: "com.example.other"},
			},
		},
		{
			PayloadUUID:       "UUID-0002",
			PayloadIdentifier: "com.example.mdm",
			PayloadContent: []types.PayloadContentItem{
				{PayloadType: "com.apple.mdm"},
			},
		},
	}

	profile, found := getEnrollmentProfile(profileLists)
	require.True(t, found, "expected enrollment profile to be found")
	assert.Equal(t, "UUID-0002", profile.PayloadUUID)
}

// TestGetEnrollmentProfile_NotFound - no enrollment profile found
func TestGetEnrollmentProfile_NotFound(t *testing.T) {
	profileLists := []types.ProfileList{
		{
			PayloadUUID: "UUID-0001",
			PayloadContent: []types.PayloadContentItem{
				{PayloadType: "com.example.wifi"},
			},
		},
	}

	_, found := getEnrollmentProfile(profileLists)
	assert.False(t, found, "expected no enrollment profile to be found")
}
