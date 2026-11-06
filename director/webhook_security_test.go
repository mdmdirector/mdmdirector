package director

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// gormErrRecordNotFound is the "no such row" error sqlmock hands back to GORM.
var gormErrRecordNotFound = gorm.ErrRecordNotFound

// Synthetic identifiers. This repository is public, so nothing here may resemble a
// real device.
const (
	testOuterUDID  = "AAAAAAAA-1111-2222-3333-444444444444"
	testOtherUDID  = "BBBBBBBB-5555-6666-7777-888888888888"
	testUserChanID = "CCCCCCCC-9999-0000-1111-222222222222"
)

// testOuterDevicePlist is a device-channel raw payload for testOuterUDID.
const testOuterDevicePlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>UDID</key>
	<string>AAAAAAAA-1111-2222-3333-444444444444</string>
	<key>SerialNumber</key>
	<string>X00SYNTHETIC</string>
</dict>
</plist>`

// testUDIDOnlyPlist carries no serial number, so UpdateDevice's serial branch is skipped.
const testUDIDOnlyPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>UDID</key>
	<string>AAAAAAAA-1111-2222-3333-444444444444</string>
</dict>
</plist>`

// testForeignDevicePlist claims a different device than the enrollment Nano authenticated.
const testForeignDevicePlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>UDID</key>
	<string>BBBBBBBB-5555-6666-7777-888888888888</string>
	<key>SerialNumber</key>
	<string>X00SYNTHETIC</string>
	<key>Lock</key>
	<true/>
	<key>UnlockPin</key>
	<string>000000</string>
</dict>
</plist>`

// testOuterUserChannelPlist is the user channel's own raw payload: it carries the
// DEVICE UDID plus a UserID, which is exactly why a whole-string comparison against
// the "<deviceUDID>:<userID>" enrollment ID breaks user-channel traffic.
const testOuterUserChannelPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>UDID</key>
	<string>AAAAAAAA-1111-2222-3333-444444444444</string>
	<key>UserID</key>
	<string>CCCCCCCC-9999-0000-1111-222222222222</string>
	<key>UserShortName</key>
	<string>tester</string>
</dict>
</plist>`

// setStringFlag registers (idempotent) and sets a global string flag for the test.
func setStringFlag(t *testing.T, name, value string) {
	t.Helper()
	if flag.Lookup(name) == nil {
		flag.String(name, "", "test")
	}
	prev := flag.Lookup(name).Value.String()
	require.NoError(t, flag.Set(name, value))
	t.Cleanup(func() { _ = flag.Set(name, prev) })
}

func setWebhookAuthFlags(t *testing.T, secret, hmacKey string, enforce bool) {
	t.Helper()
	if flag.Lookup("prometheus") == nil {
		flag.Bool("prometheus", false, "Enable prometheus metrics")
	}
	setStringFlag(t, "webhook-secret", secret)
	setStringFlag(t, "webhook-hmac-key", hmacKey)
	setBoolFlag(t, "webhook-auth-enforce", enforce)
}

func webhookRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// ---- 1. webhook authentication --------------------------------------------------

// /webhook must not be an anonymous state-changing route. This increment ships
// warn-and-allow by default so Nano's -webhook-url can be rolled independently;
// with enforcement on, an unauthenticated POST must be rejected outright.
func TestWebhookHandler_RequiresAuth(t *testing.T) {
	const secret = "synthetic-webhook-secret"
	body := []byte(`{"topic":"mdm.Connect"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	goodHMAC := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name       string
		secret     string
		hmacKey    string
		enforce    bool
		header     map[string]string
		basicAuth  []string
		wantStatus int
		wantCalled bool
	}{
		{
			name:       "no credential, enforcement on, rejected",
			secret:     secret,
			enforce:    true,
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
		{
			name:       "wrong secret, enforcement on, rejected",
			secret:     secret,
			enforce:    true,
			header:     map[string]string{WebhookSecretHeader: "wrong"},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
		{
			name:       "no credential, staged default, warn and allow",
			secret:     secret,
			enforce:    false,
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "no secret configured, staged default, warn and allow",
			enforce:    false,
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "no secret configured, enforcement on, rejected",
			enforce:    true,
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
		{
			name:       "correct secret header accepted",
			secret:     secret,
			enforce:    true,
			header:     map[string]string{WebhookSecretHeader: secret},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "correct secret as basic auth password accepted",
			secret:     secret,
			enforce:    true,
			basicAuth:  []string{"mdmdirector", secret},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "correct nanomdm hmac signature accepted",
			hmacKey:    secret,
			enforce:    true,
			header:     map[string]string{WebhookHMACHeader: goodHMAC},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "wrong hmac signature rejected",
			hmacKey:    secret,
			enforce:    true,
			header:     map[string]string{WebhookHMACHeader: base64.StdEncoding.EncodeToString([]byte("nope"))},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setWebhookAuthFlags(t, tt.secret, tt.hmacKey, tt.enforce)

			called := false
			var seenBody []byte
			inner := func(w http.ResponseWriter, r *http.Request) {
				called = true
				seenBody, _ = io.ReadAll(r.Body)
			}

			req := webhookRequest(t, body)
			for k, v := range tt.header {
				req.Header.Set(k, v)
			}
			if tt.basicAuth != nil {
				req.SetBasicAuth(tt.basicAuth[0], tt.basicAuth[1])
			}
			rr := httptest.NewRecorder()

			WebhookAuth(inner)(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)
			assert.Equal(t, tt.wantCalled, called)
			if tt.wantCalled {
				assert.Equal(t, body, seenBody, "the handler must still see the whole body")
			}
		})
	}
}

// ---- 2. outer-vs-raw UDID binding -----------------------------------------------

func TestDeviceUDIDFromEnrollmentID(t *testing.T) {
	assert.Equal(t, testOuterUDID, deviceUDIDFromEnrollmentID(testOuterUDID))
	assert.Equal(t, testOuterUDID, deviceUDIDFromEnrollmentID(testOuterUDID+":"+testUserChanID))
	assert.Equal(t, "", deviceUDIDFromEnrollmentID(""))
}

// A raw payload claiming another device must error out before any row for that
// claimed UDID is read or written. No sqlmock expectations are registered, so any
// query at all fails the test.
func TestHandleAcknowledgeEvent_RejectsRawUDIDMismatch(t *testing.T) {
	mockSpy, teardown := setupMockDB(t)
	defer teardown()

	event := &types.AcknowledgeEvent{
		UDID:       testOuterUDID,
		Status:     "Idle",
		RawPayload: []byte(testForeignDevicePlist),
	}

	err := handleAcknowledgeEvent(event)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "bindDeviceToEnrollment")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestHandleCheckinEvent_RejectsRawUDIDMismatch(t *testing.T) {
	mockSpy, teardown := setupMockDB(t)
	defer teardown()

	event := &types.CheckinEvent{
		UDID:       testOuterUDID,
		RawPayload: []byte(testForeignDevicePlist),
	}

	err := handleCheckinEvent("mdm.TokenUpdate", event)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "bindDeviceToEnrollment")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// The guard against over-fixing. Nano's user-channel enrollment ID is
// "<deviceUDID>:<userID>" and the user channel's own payload carries the bare
// device UDID, so whole-string equality would reject every user-channel message
// fleet-wide.
func TestHandleCheckinEvent_AcceptsUserChannelID(t *testing.T) {
	mockSpy, teardown := setupMockDB(t)
	defer teardown()

	event := &types.CheckinEvent{
		UDID:         "",
		EnrollmentID: testOuterUDID + ":" + testUserChanID,
		RawPayload:   []byte(testOuterUserChannelPlist),
	}

	err := handleCheckinEvent("mdm.TokenUpdate", event)

	assert.NoError(t, err)
	// isUserChannel short-circuits before any query; a mismatch rejection would have
	// returned an error instead.
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// With the raw UDID empty the outer value is forced in, so the serial-only lookup
// path cannot run against an attacker-chosen serial.
func TestBindDeviceToEnrollment_ForcesEmptyRawUDID(t *testing.T) {
	device := types.Device{SerialNumber: "X00SYNTHETIC"}

	err := bindDeviceToEnrollment(&device, "", testOuterUDID+":"+testUserChanID)

	require.NoError(t, err)
	assert.Equal(t, testOuterUDID, device.UDID)
}

// ---- 3. the Status == "Idle" side effect ----------------------------------------

// A legitimate Nano Idle report carries an EMPTY command UUID (see nanomdm
// test/e2e/queue.go), so the gate is "this device row already existed". An Idle for
// an unknown device must not reach RequestDeviceUpdate -> EraseLockDevice, and an
// Idle quoting a command UUID belonging to another device must not either.
func TestHandleAcknowledgeEvent_IdleRequiresKnownCommand(t *testing.T) {
	setWebhookAuthFlags(t, "", "", false)
	setBoolFlag(t, "push-new-build", false)
	mockSpy, teardown := setupMockDB(t)
	defer teardown()

	hook := test.NewGlobal()
	defer hook.Reset()

	// The pre-existence lookup misses, then UpdateDevice creates the row.
	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1`).
		WillReturnError(gormErrRecordNotFound)
	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1`).
		WillReturnError(gormErrRecordNotFound)
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`INSERT INTO "devices"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()
	// Nothing further may run: no lock/erase select, no info-request claim.

	event := &types.AcknowledgeEvent{
		UDID:       testOuterUDID,
		Status:     "Idle",
		RawPayload: []byte(testUDIDOnlyPlist),
	}

	err := handleAcknowledgeEvent(event)

	assert.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())

	var gated bool
	for _, entry := range hook.Entries {
		if entry.Level == logrus.WarnLevel &&
			strings.Contains(entry.Message, "no pre-existing record") {
			gated = true
		}
	}
	assert.True(t, gated, "the Idle side effect must have been refused for the unknown device")
}

// An Idle report quoting a command UUID that belongs to no command of this device
// must not drive the side effect either.
func TestIdleSideEffectAllowed_ForeignCommandUUID(t *testing.T) {
	setWebhookAuthFlags(t, "", "", false)
	mockSpy, teardown := setupMockDB(t)
	defer teardown()

	mockSpy.ExpectQuery(`SELECT "command_uuid" FROM "commands" WHERE command_uuid = \$1 AND device_ud_id = \$2`).
		WithArgs("11111111-2222-3333-4444-555555555555", testOuterUDID).
		WillReturnError(gormErrRecordNotFound)

	event := &types.AcknowledgeEvent{
		UDID:        testOuterUDID,
		Status:      "Idle",
		CommandUUID: "11111111-2222-3333-4444-555555555555",
	}

	allowed := idleSideEffectAllowed(event, types.Device{UDID: testOuterUDID}, true)

	assert.False(t, allowed)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// An Idle report with an EMPTY command UUID is what a real device sends, so a known
// device must still be allowed through. This is the guard against breaking the
// periodic info refresh fleet-wide.
func TestIdleSideEffectAllowed_KnownDeviceEmptyCommandUUID(t *testing.T) {
	setWebhookAuthFlags(t, "", "", false)
	mockSpy, teardown := setupMockDB(t)
	defer teardown()

	event := &types.AcknowledgeEvent{UDID: testOuterUDID, Status: "Idle"}

	allowed := idleSideEffectAllowed(event, types.Device{UDID: testOuterUDID}, true)

	assert.True(t, allowed)
	// No query at all: no expectations were registered.
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// ---- 4. generatePin ignores devices.unlock_pin -----------------------------------

// devices.unlock_pin is writable from the webhook's raw payload via UpdateDevice's
// GORM Assign, so an attacker-chosen PIN was returned verbatim and escrowed as
// though legitimate. The PIN must always come from the unlock_pins table.
func TestGeneratePin_IgnoresDeviceUnlockPinColumn(t *testing.T) {
	if flag.Lookup("debug") == nil {
		flag.Bool("debug", false, "")
	}
	mockSpy, teardown := setupMockDB(t)
	defer teardown()

	mockSpy.ExpectQuery(`SELECT \* FROM "unlock_pins"`).
		WillReturnRows(sqlmock.NewRows([]string{"device_ud_id", "unlock_pin"}).
			AddRow(testOuterUDID, "135790"))

	pin, err := generatePin(types.Device{UDID: testOuterUDID, UnlockPin: "000000"})

	require.NoError(t, err)
	assert.Equal(t, "135790", pin, "the attacker-supplied devices.unlock_pin must be ignored")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
