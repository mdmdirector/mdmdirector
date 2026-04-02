package director

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// A minimal valid device plist payload for tests.
// Only carries UDID and SerialNumber — enough for most checkin/acknowledge flows.
const testDevicePlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>UDID</key>
	<string>1234-5678-123456</string>
	<key>SerialNumber</key>
	<string>C02ABCDEFGH</string>
</dict>
</plist>`

// errDBGoneAway is a sentinel error used in DB failure tests.
var errDBGoneAway = fmt.Errorf("database has gone away")

// ---- reconcileDeviceState -------------------------------------------------------

// When neither trigger condition is met the function is a clean no-op.
func TestReconcileDeviceState_NeitherConditionMet(t *testing.T) {
	device := types.Device{UDID: "1234-5678-123456", SerialNumber: "C02ABCDEFGH"}
	currentDevice := &types.Device{
		InitialTasksRun:       true,
		TokenUpdateRecieved:   true,
		AwaitingConfiguration: false,
	}

	done, err := reconcileDeviceState(device, currentDevice)

	assert.False(t, done)
	assert.NoError(t, err)
}

// TokenUpdateRecieved=false → RunInitialTasks must NOT be triggered.
func TestReconcileDeviceState_TokenUpdateNotReceived(t *testing.T) {
	device := types.Device{UDID: "1234-5678-123456"}
	currentDevice := &types.Device{
		InitialTasksRun:     false,
		TokenUpdateRecieved: false,
	}

	done, err := reconcileDeviceState(device, currentDevice)

	assert.False(t, done)
	assert.NoError(t, err)
}

// InitialTasksRun=true → the first condition is false; RunInitialTasks must NOT be re-triggered.
func TestReconcileDeviceState_InitialTasksAlreadyRun(t *testing.T) {
	device := types.Device{UDID: "1234-5678-123456"}
	currentDevice := &types.Device{
		InitialTasksRun:     true,
		TokenUpdateRecieved: true,
	}

	done, err := reconcileDeviceState(device, currentDevice)

	assert.False(t, done)
	assert.NoError(t, err)
}

// AwaitingConfiguration=false → SendDeviceConfigured must NOT be called even when InitialTasksRun=true.
func TestReconcileDeviceState_NotAwaitingConfiguration(t *testing.T) {
	device := types.Device{UDID: "1234-5678-123456"}
	currentDevice := &types.Device{
		InitialTasksRun:       true,
		AwaitingConfiguration: false,
	}

	done, err := reconcileDeviceState(device, currentDevice)

	assert.False(t, done)
	assert.NoError(t, err)
}

// ---- WebhookHandler HTTP routing ------------------------------------------------

// Only CheckinEvent is populated — AcknowledgeEvent is nil.
// WebhookHandler must route to handleCheckinEvent and return 200 regardless of inner errors.
func TestWebhookHandler_OnlyCheckinEventPopulated(t *testing.T) {
	payload := types.PostPayload{
		Topic: "mdm.TokenUpdate",
		CheckinEvent: &types.CheckinEvent{
			UDID:       "1234-5678-123456",
			RawPayload: []byte("invalid plist"),
		},
		// AcknowledgeEvent intentionally nil
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	WebhookHandler(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

// Only AcknowledgeEvent is populated — CheckinEvent is nil.
// WebhookHandler must route to handleAcknowledgeEvent and return 200 regardless of inner errors.
func TestWebhookHandler_OnlyAcknowledgeEventPopulated(t *testing.T) {
	payload := types.PostPayload{
		AcknowledgeEvent: &types.AcknowledgeEvent{
			UDID:       "1234-5678-123456",
			RawPayload: []byte("invalid plist"),
		},
		// CheckinEvent intentionally nil
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	WebhookHandler(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

// ---- handleCheckinEvent ---------------------------------------------------------

func TestHandleCheckinEvent_InvalidPlist(t *testing.T) {
	event := &types.CheckinEvent{
		RawPayload: []byte("not valid plist data"),
	}

	err := handleCheckinEvent("mdm.TokenUpdate", event)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "handleCheckinEvent:plist.Unmarshal")
}

// mdm.CheckOut resets the device and returns immediately — no UpdateDevice call.
func TestHandleCheckinEvent_CheckOut_ResetsDeviceAndReturnsEarly(t *testing.T) {
	utils.FlagProvider = mockFlagBuilder{false}

	postgresMock, mockSpy, _ := sqlmock.New()
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	// ClearCommands: DELETE pending commands
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^DELETE FROM "commands" WHERE device_ud_id = \$1 AND NOT \(status = \$2 OR status = \$3\)`).
		WithArgs("1234-5678-123456", "Error", "Acknowledged").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mockSpy.ExpectCommit()

	// ResetDevice: UPDATE device flags
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^UPDATE "devices"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	event := &types.CheckinEvent{
		UDID:       "1234-5678-123456",
		RawPayload: []byte(testDevicePlist),
	}

	err := handleCheckinEvent("mdm.CheckOut", event)

	assert.NoError(t, err)
}

// mdm.CheckOut: if ClearCommands fails the error must propagate.
func TestHandleCheckinEvent_CheckOut_PropagatesResetDeviceError(t *testing.T) {
	utils.FlagProvider = mockFlagBuilder{false}

	postgresMock, mockSpy, _ := sqlmock.New()
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{SkipDefaultTransaction: true})
	db.DB = DB

	mockSpy.ExpectExec(`.*`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(errDBGoneAway)

	event := &types.CheckinEvent{
		UDID:       "1234-5678-123456",
		RawPayload: []byte(testDevicePlist),
	}

	err := handleCheckinEvent("mdm.CheckOut", event)

	require.Error(t, err)
}

// ---- handleAcknowledgeEvent -----------------------------------------------------

func TestHandleAcknowledgeEvent_InvalidPlist(t *testing.T) {
	event := &types.AcknowledgeEvent{
		RawPayload: []byte("not valid plist data"),
	}

	err := handleAcknowledgeEvent(event)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "handleAcknowledgeEvent:plist.Unmarshal")
}

// ---- processAcknowledgePayload --------------------------------------------------

// ProfileList key in the payload dict must route to profile-verification logic.
// An invalid plist in RawPayload surfaces the Unmarshal error path.
func TestProcessAcknowledgePayload_ProfileList_InvalidPlist(t *testing.T) {
	device := types.Device{UDID: "1234-5678-123456", SerialNumber: "C02ABCDEFGH"}
	event := &types.AcknowledgeEvent{
		RawPayload: []byte("not valid plist"),
	}
	payloadDict := map[string]interface{}{
		"ProfileList": []interface{}{},
	}

	err := processAcknowledgePayload(event, device, payloadDict)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "processAcknowledgePayload:ProfileList:plist.Unmarshal")
}

// SecurityInfo key in the payload dict must route to security-info save logic.
func TestProcessAcknowledgePayload_SecurityInfo_InvalidPlist(t *testing.T) {
	device := types.Device{UDID: "1234-5678-123456", SerialNumber: "C02ABCDEFGH"}
	event := &types.AcknowledgeEvent{
		RawPayload: []byte("not valid plist"),
	}
	payloadDict := map[string]interface{}{
		"SecurityInfo": map[string]interface{}{},
	}

	err := processAcknowledgePayload(event, device, payloadDict)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "processAcknowledgePayload:SecurityInfo:plist.Unmarshal")
}

// CertificateList key in the payload dict must route to certificate-list processing.
func TestProcessAcknowledgePayload_CertificateList_InvalidPlist(t *testing.T) {
	device := types.Device{UDID: "1234-5678-123456", SerialNumber: "C02ABCDEFGH"}
	event := &types.AcknowledgeEvent{
		RawPayload: []byte("not valid plist"),
	}
	payloadDict := map[string]interface{}{
		"CertificateList": []interface{}{},
	}

	err := processAcknowledgePayload(event, device, payloadDict)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "processAcknowledgePayload:CertificateList:plist.Unmarshal")
}

// QueryResponses key in the payload dict must route to device-info update logic.
func TestProcessAcknowledgePayload_QueryResponses_InvalidPlist(t *testing.T) {
	device := types.Device{UDID: "1234-5678-123456", SerialNumber: "C02ABCDEFGH"}
	event := &types.AcknowledgeEvent{
		RawPayload: []byte("not valid plist"),
	}
	payloadDict := map[string]interface{}{
		"QueryResponses": map[string]interface{}{},
	}

	err := processAcknowledgePayload(event, device, payloadDict)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "processAcknowledgePayload:QueryResponses:plist.Unmarshal")
}
