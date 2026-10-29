package director

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// A minimal valid device plist payload for tests.
// Only carries UDID and SerialNumber - enough for most checkin/acknowledge flows.
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
	currentDevice := &types.Device{
		UDID:                  "1234-5678-123456",
		SerialNumber:          "C02ABCDEFGH",
		InitialTasksRun:       true,
		TokenUpdateRecieved:   true,
		AwaitingConfiguration: false,
	}

	err := reconcileDeviceState(currentDevice)

	assert.NoError(t, err)
}

// TokenUpdateRecieved=false → RunInitialTasks must NOT be triggered.
func TestReconcileDeviceState_TokenUpdateNotReceived(t *testing.T) {
	currentDevice := &types.Device{
		UDID:                "1234-5678-123456",
		InitialTasksRun:     false,
		TokenUpdateRecieved: false,
	}

	err := reconcileDeviceState(currentDevice)

	assert.NoError(t, err)
}

// InitialTasksRun=true → the first condition is false; RunInitialTasks must NOT be re-triggered.
func TestReconcileDeviceState_InitialTasksAlreadyRun(t *testing.T) {
	currentDevice := &types.Device{
		UDID:                "1234-5678-123456",
		InitialTasksRun:     true,
		TokenUpdateRecieved: true,
	}

	err := reconcileDeviceState(currentDevice)

	assert.NoError(t, err)
}

// A lease start time younger than the TTL means another invocation is still running
// RunInitialTasks (typically: this event is one of its own command acknowledgements).
// reconcileDeviceState must return without touching the DB - no lease UPDATE, no
// lease_contention count. No mock DB is installed, so any DB access would fail the test.
func TestReconcileDeviceState_InitialTasksInFlightSkips(t *testing.T) {
	started := time.Now().Add(-1 * time.Minute)
	currentDevice := &types.Device{
		UDID:                     "1234-5678-123456",
		SerialNumber:             "C02ABCDEFGH",
		InitialTasksRun:          false,
		TokenUpdateRecieved:      true,
		RunInitialTasksStarttime: &started,
	}

	err := reconcileDeviceState(currentDevice)

	assert.NoError(t, err)
}

// A lease start time older than the TTL is a holder that died mid-run. The in-flight
// check must NOT short-circuit; RunInitialTasks runs and attempts the lease UPDATE.
func TestReconcileDeviceState_StaleInitialTasksLeaseRetries(t *testing.T) {
	mock, teardown := setupMockDB(t)
	defer teardown()

	// RunInitialTasks tries the lease; sqlmock returns 0 rows so it stops right there.
	mock.ExpectExec(acquireSQL).
		WithArgs("1234-5678-123456").
		WillReturnResult(sqlmock.NewResult(0, 0))

	started := time.Now().Add(-initialTasksLeaseDuration - time.Minute)
	currentDevice := &types.Device{
		UDID:                     "1234-5678-123456",
		SerialNumber:             "C02ABCDEFGH",
		InitialTasksRun:          false,
		TokenUpdateRecieved:      true,
		RunInitialTasksStarttime: &started,
	}

	err := reconcileDeviceState(currentDevice)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet(), "expected RunInitialTasks to attempt the lease")
}

// AwaitingConfiguration=false → SendDeviceConfigured must NOT be called even when InitialTasksRun=true.
func TestReconcileDeviceState_NotAwaitingConfiguration(t *testing.T) {
	currentDevice := &types.Device{
		UDID:                  "1234-5678-123456",
		InitialTasksRun:       true,
		AwaitingConfiguration: false,
	}

	err := reconcileDeviceState(currentDevice)

	assert.NoError(t, err)
}

// ---- WebhookHandler HTTP routing ------------------------------------------------

// Only CheckinEvent is populated - AcknowledgeEvent is nil.
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

// Only AcknowledgeEvent is populated - CheckinEvent is nil.
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

// mdm.CheckOut resets the device and returns immediately - no UpdateDevice call.
func TestHandleCheckinEvent_CheckOut_ResetsDeviceAndReturnsEarly(t *testing.T) {
	utils.FlagProvider = mockFlagBuilder{false}

	postgresMock, mockSpy, _ := sqlmock.New()
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	// ClearCommands: DELETE pending commands
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^DELETE FROM "commands" WHERE device_ud_id = \$1 AND NOT status IN \(\$2,\$3,\$4\)`).
		WithArgs("1234-5678-123456", "Error", "Acknowledged", commandStatusRetriedViaDDM).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mockSpy.ExpectCommit()

	// ResetDevice: DELETE the DDM opt-in row, before the flags so a concurrent
	// RunInitialTasks never sees the old enrollment's opt-in
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^DELETE FROM "ddm_opt_ins" WHERE device_ud_id = \$1`).
		WithArgs("1234-5678-123456").
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
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// expectResetUntilOptInDelete mocks every statement ResetDevice issues, in the order the
// code must issue them: the opt-in row goes before the flag reset, because the moment
// initial_tasks_run is false a TokenUpdate on another replica can start RunInitialTasks,
// which must not see the previous enrollment's opt-in
func expectResetUntilOptInDelete(mockSpy sqlmock.Sqlmock, udid string, optInRows int64) {
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^DELETE FROM "commands" WHERE device_ud_id = \$1`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mockSpy.ExpectCommit()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^DELETE FROM "ddm_opt_ins" WHERE device_ud_id = \$1`).
		WithArgs(udid).
		WillReturnResult(sqlmock.NewResult(0, optInRows))
	mockSpy.ExpectCommit()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^UPDATE "devices"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()
}

// ResetDevice never touches declarations, whatever the DDM flags say: RunInitialTasks may
// already be running for the new enrollment, and a teardown from here would race the
// declarations it writes. resetDDMForEnrollment, inside RunInitialTasks, owns that
// cleanup. So the opt-in delete and the flag reset are all that happens, in that order.
func TestResetDevice_DoesNotTearDownDeclarations(t *testing.T) {
	for _, useDDM := range []bool{true, false} {
		t.Run(fmt.Sprintf("use-ddm=%v", useDDM), func(t *testing.T) {
			setupDDMFlags(t, useDDM, useDDM)
			postgresMock, mockSpy, _ := sqlmock.New()
			defer postgresMock.Close()
			db.DB, _ = gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})

			udid := "1234-5678-123456"
			expectResetUntilOptInDelete(mockSpy, udid, 1)

			err := ResetDevice(types.Device{UDID: udid, SerialNumber: "SERIAL"})

			require.NoError(t, err)
			assert.NoError(t, mockSpy.ExpectationsWereMet())
		})
	}
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

// ---- previousBuildVersion -------------------------------------------------------

// Unknown UDID (no row) returns "" — used by handlers to skip the build comparison
// on first enrollment.
func TestPreviousBuildVersion_UnknownDeviceReturnsEmpty(t *testing.T) {
	postgresMock, mockSpy, _ := sqlmock.New()
	defer postgresMock.Close()
	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	mockSpy.ExpectQuery(`^SELECT \* FROM "devices" WHERE ud_id = \$1`).
		WillReturnError(gorm.ErrRecordNotFound)

	got := previousBuildVersion("UNKNOWN-UDID")

	assert.Equal(t, "", got)
}

// Known device returns its persisted BuildVersion.
func TestPreviousBuildVersion_KnownDeviceReturnsBuild(t *testing.T) {
	postgresMock, mockSpy, _ := sqlmock.New()
	defer postgresMock.Close()
	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	mockSpy.ExpectQuery(`^SELECT \* FROM "devices" WHERE ud_id = \$1`).
		WillReturnRows(sqlmock.NewRows([]string{"ud_id", "build_version"}).
			AddRow("1234-5678-123456", "25F71"))

	got := previousBuildVersion("1234-5678-123456")

	assert.Equal(t, "25F71", got)
}

// ---- pushOnNewBuild -------------------------------------------------------------

// setBoolFlag registers (idempotent) and sets a global bool flag for the
// duration of the test. Used to drive utils.PushOnNewBuild() / utils.UseDDM().
func setBoolFlag(t *testing.T, name string, enabled bool) {
	t.Helper()
	if flag.Lookup(name) == nil {
		flag.Bool(name, enabled, "test")
	}
	prev := flag.Lookup(name).Value.String()
	require.NoError(t, flag.Set(name, fmt.Sprintf("%t", enabled)))
	t.Cleanup(func() { _ = flag.Set(name, prev) })
}

func setPushOnNewBuildFlag(t *testing.T) {
	setBoolFlag(t, "push-new-build", true)
}

// First enrollment: no prior build to compare against → no-op, no error.
func TestPushOnNewBuild_FirstEnrollmentNoOp(t *testing.T) {
	setPushOnNewBuildFlag(t)

	device := types.Device{UDID: "1234-5678-123456"}

	err := pushOnNewBuild(device, "", "25F71")

	assert.NoError(t, err)
}

// Same build on both sides (the original bug pattern): no-op, no error,
// no profile push attempted.
func TestPushOnNewBuild_SameBuildNoOp(t *testing.T) {
	setPushOnNewBuildFlag(t)

	device := types.Device{UDID: "1234-5678-123456"}

	err := pushOnNewBuild(device, "25F71", "25F71")

	assert.NoError(t, err)
}

// Apparent downgrade: skipped (avoids spurious re-pushes from synthetic webhooks
// or rollback scenarios).
func TestPushOnNewBuild_DowngradeNoOp(t *testing.T) {
	setPushOnNewBuildFlag(t)

	device := types.Device{UDID: "1234-5678-123456"}

	err := pushOnNewBuild(device, "26Z99", "25F71")

	assert.NoError(t, err)
}

// Build upgrade triggers InstallAllProfiles. Asserted by observing that
// InstallAllProfiles' first query (device-specific profiles by UDID) was
// issued — proves the version-comparison branch fired. We let every query
// fail; pushOnNewBuild swallows the resulting error and returns nil, which
// matches its production contract (errors logged, not propagated).
func TestPushOnNewBuild_BuildUpgradeTriggersInstall(t *testing.T) {
	setPushOnNewBuildFlag(t)
	setBoolFlag(t, "use-ddm", false) // InstallAllProfiles reads utils.UseDDM()

	postgresMock, mockSpy, _ := sqlmock.New()
	defer postgresMock.Close()
	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	mockSpy.MatchExpectationsInOrder(false)
	deviceProfilesQuery := mockSpy.ExpectQuery(`^SELECT \* FROM "device_profiles" WHERE device_ud_id = \$1`).
		WithArgs("1234-5678-123456").
		WillReturnError(errDBGoneAway)
	// Allow any number of follow-up queries (shared profiles, RequestProfileList)
	// to fail without failing the test — only the device_profiles query is
	// load-bearing for the assertion.
	mockSpy.ExpectQuery(`.*`).WillReturnError(errDBGoneAway)
	mockSpy.ExpectQuery(`.*`).WillReturnError(errDBGoneAway)

	device := types.Device{UDID: "1234-5678-123456", InitialTasksRun: true}

	err := pushOnNewBuild(device, "25F71", "26Z99")

	assert.NoError(t, err)
	require.NotNil(t, deviceProfilesQuery, "device_profiles query must have been registered")
}

// ---- user channel ----------------------------------------------------------------

// A user-channel message carries the device's UDID plus a UserID. On macOS this is
// the console user's channel.
const testUserChannelPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>UDID</key>
	<string>1234-5678-123456</string>
	<key>UserID</key>
	<string>A1B2C3D4-0000-0000-0000-000000000000</string>
	<key>UserShortName</key>
	<string>tester</string>
</dict>
</plist>`

func TestIsUserChannel(t *testing.T) {
	assert.False(t, isUserChannel([]byte(testDevicePlist)))
	assert.True(t, isUserChannel([]byte(testUserChannelPlist)))
	assert.False(t, isUserChannel([]byte("not valid plist data")))
}

// A user-channel TokenUpdate must not mark the device's TokenUpdate received: that
// would start RunInitialTasks before nanomdm stores the device channel's new PushMagic.
// The same goes for every other user-channel check-in, CheckOut included.
func TestHandleCheckinEvent_UserChannelIgnored(t *testing.T) {
	for _, topic := range []string{"mdm.TokenUpdate", "mdm.CheckOut", "mdm.Authenticate"} {
		t.Run(topic, func(t *testing.T) {
			postgresMock, mockSpy, _ := sqlmock.New()
			defer postgresMock.Close()

			DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
			db.DB = DB

			event := &types.CheckinEvent{
				UDID:       "1234-5678-123456",
				RawPayload: []byte(testUserChannelPlist),
			}

			err := handleCheckinEvent(topic, event)

			assert.NoError(t, err)
			// No expectations were set, so any query fails this.
			assert.NoError(t, mockSpy.ExpectationsWereMet())
		})
	}
}

// A build upgrade on a device whose initial tasks are still pending must not push:
// RunInitialTasks owns that push, and until it has cleared the old enrollment's DDM
// declarations a push from here would let the device sync them. No DB query may run.
func TestPushOnNewBuild_InitialTasksPendingNoOp(t *testing.T) {
	setPushOnNewBuildFlag(t)

	postgresMock, mockSpy, _ := sqlmock.New()
	defer postgresMock.Close()
	db.DB, _ = gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})

	device := types.Device{UDID: "1234-5678-123456", InitialTasksRun: false}

	err := pushOnNewBuild(device, "25F71", "26Z99")

	assert.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// ---- DeviceConfigured resend clears awaiting_configuration -----------------------

// newDeviceConfiguredServer stands in for NanoMDM's enqueue endpoint and counts the
// DeviceConfigured commands it receives.
func newDeviceConfiguredServer(t *testing.T, udid string) *int {
	t.Helper()
	sends := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		resp := mdm.APIResponse{
			CommandUUID: fmt.Sprintf("device-configured-%d", sends),
			RequestType: "DeviceConfigured",
			Status:      map[string]mdm.EnrollmentStatus{udid: {PushResult: "success"}},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)
	mdm.InitClient(server.URL, "test-api-key")
	return &sends
}

// The resend path is the loop: a device whose row still says
// awaiting_configuration=true after initial tasks ran gets DeviceConfigured on every
// event, and each send's own acks are events. Sending must clear the flag in the same
// call, in the row and on the device the caller holds, so the next event is a no-op.
func TestReconcileDeviceState_ResendClearsAwaitingConfiguration(t *testing.T) {
	setupNanoMDMFlag(t)
	mock, teardown := setupMockDB(t)
	defer teardown()
	sends := newDeviceConfiguredServer(t, "1234-5678-123456")

	// SendDeviceConfigured sends twice; each send loads the device and records a row.
	for i := 0; i < 2; i++ {
		mockGetDevice(mock, "1234-5678-123456")
		mockCreateCommand(mock)
	}
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "devices" SET "awaiting_configuration"=\$1,"updated_at"=\$2 WHERE ud_id = \$3`).
		WithArgs(false, sqlmock.AnyArg(), "1234-5678-123456").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	currentDevice := &types.Device{
		UDID:                  "1234-5678-123456",
		SerialNumber:          "C02ABCDEFGH",
		InitialTasksRun:       true,
		AwaitingConfiguration: true,
	}

	err := reconcileDeviceState(currentDevice)

	require.NoError(t, err)
	assert.Equal(t, 2, *sends, "DeviceConfigured sent twice for luck")
	assert.False(t, currentDevice.AwaitingConfiguration, "in-memory flag cleared for the rest of this event")
	assert.NoError(t, mock.ExpectationsWereMet())

	// The device's next event - typically the NotNow or Acknowledged for the send above -
	// reaches here with the cleared row and sends nothing. No DB expectations remain and
	// a send would hit the mock DB, so any resend fails the test.
	err = reconcileDeviceState(currentDevice)
	require.NoError(t, err)
	assert.Equal(t, 2, *sends, "no resend on the following event")
}

// A failed send leaves the flag set so the next event retries; the flag is only cleared
// once DeviceConfigured is actually on its way.
func TestReconcileDeviceState_FailedResendKeepsAwaitingConfiguration(t *testing.T) {
	setupNanoMDMFlag(t)
	mock, teardown := setupMockDB(t)
	defer teardown()

	// GetDevice fails before anything reaches NanoMDM.
	mock.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1`).WillReturnError(errDBGoneAway)

	currentDevice := &types.Device{
		UDID:                  "1234-5678-123456",
		InitialTasksRun:       true,
		AwaitingConfiguration: true,
	}

	err := reconcileDeviceState(currentDevice)

	assert.ErrorIs(t, err, errDBGoneAway)
	assert.True(t, currentDevice.AwaitingConfiguration)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// SaveDeviceConfigured runs at the end of initial tasks, right after the first
// DeviceConfigured. It must clear awaiting_configuration along with the other flags so a
// freshly enrolled device doesn't enter the resend path on its very next ack.
func TestSaveDeviceConfigured_ClearsAwaitingConfiguration(t *testing.T) {
	mock, teardown := setupMockDB(t)
	defer teardown()

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "devices" SET .*"awaiting_configuration"=\$\d+.* WHERE ud_id = \$\d+`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := SaveDeviceConfigured(types.Device{UDID: "1234-5678-123456"})

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
