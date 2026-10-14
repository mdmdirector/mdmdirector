package director

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	retryTestUDID      = "test-udid-retry"
	retryTestCmdUUID   = "failed-command-uuid"
	retryTestProfileID = "com.example.retry"
	retryTestHash      = "hash-retry"
)

// setupRetryFlags registers the flags the retry path and the MicroMDM push path read, sets
// the retry limit, and points the MicroMDM client at a stub that answers every enqueue
// with a fixed command UUID.
// stubRetrySleep replaces the backoff sleep with a recorder and returns the recorded waits.
func stubRetrySleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var waits []time.Duration
	old := retrySleep
	retrySleep = func(d time.Duration) { waits = append(waits, d) }
	t.Cleanup(func() { retrySleep = old })
	return &waits
}

func setupRetryFlags(t *testing.T, retries int) {
	t.Helper()
	setupDDMFlags(t, false, false)

	if flag.Lookup("install-profile-retries") == nil {
		flag.Int("install-profile-retries", 2, "retries")
	}
	if flag.Lookup("install-profile-total-retries") == nil {
		flag.Int("install-profile-total-retries", 5, "total retries")
	}
	if flag.Lookup("mdm-server-type") == nil {
		flag.String("mdm-server-type", "micromdm", "server type")
	}
	if flag.Lookup("sign") == nil {
		flag.Bool("sign", false, "sign profiles")
	}
	if flag.Lookup("micromdmurl") == nil {
		flag.String("micromdmurl", "", "MicroMDM Server URL")
	}
	if flag.Lookup("micromdmapikey") == nil {
		flag.String("micromdmapikey", "", "MicroMDM Server API Key")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.CommandResponse{}
		resp.Payload.CommandUUID = "retry-command-uuid"
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)

	require.NoError(t, flag.Set("install-profile-retries", itoa(retries)))
	require.NoError(t, flag.Set("install-profile-total-retries", itoa(retries+2)))
	require.NoError(t, flag.Set("mdm-server-type", "micromdm"))
	require.NoError(t, flag.Set("sign", "false"))
	require.NoError(t, flag.Set("micromdmurl", server.URL))
	require.NoError(t, flag.Set("micromdmapikey", "test-key"))
}

func itoa(n int) string {
	return fmt.Sprint(n)
}

// mockFailedCommand is the lookup of the command the device answered with Error.
func mockFailedCommand(mockSpy sqlmock.Sqlmock, attemptCount int) {
	rows := sqlmock.NewRows([]string{"command_uuid", "status", "device_ud_id", "request_type", "identifier", "content_hash", "attempt_count"}).
		AddRow(retryTestCmdUUID, "Error", retryTestUDID, "InstallProfile", retryTestProfileID, retryTestHash, attemptCount)
	mockSpy.ExpectQuery(`SELECT \* FROM "commands" WHERE device_ud_id = \$1 AND command_uuid = \$2 ORDER BY "commands"\."command_uuid" LIMIT 1`).
		WithArgs(retryTestUDID, retryTestCmdUUID).
		WillReturnRows(rows)
}

func mockNotOptedIntoDDM(mockSpy sqlmock.Sqlmock) {
	mockSpy.ExpectQuery(`SELECT count\(\*\) FROM "ddm_opt_ins" WHERE device_ud_id = \$1`).
		WithArgs(retryTestUDID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
}

// mockDeviceProfile answers the device-specific profile lookup. An empty hash means
// "no such row".
func mockDeviceProfile(mockSpy sqlmock.Sqlmock, hash string) {
	query := mockSpy.ExpectQuery(`SELECT \* FROM "device_profiles" WHERE device_ud_id = \$1 AND payload_identifier = \$2 AND installed = \$3`).
		WithArgs(retryTestUDID, retryTestProfileID, true)
	if hash == "" {
		query.WillReturnRows(sqlmock.NewRows([]string{"payload_identifier"}))
		return
	}
	query.WillReturnRows(sqlmock.NewRows([]string{"payload_identifier", "device_ud_id", "hashed_payload_uuid", "mobileconfig_data", "installed"}).
		AddRow(retryTestProfileID, retryTestUDID, hash, []byte("<plist/>"), true))
}

func mockSharedProfile(mockSpy sqlmock.Sqlmock, hash string) {
	query := mockSpy.ExpectQuery(`SELECT \* FROM "shared_profiles" WHERE payload_identifier = \$1 AND installed = \$2`).
		WithArgs(retryTestProfileID, true)
	if hash == "" {
		query.WillReturnRows(sqlmock.NewRows([]string{"payload_identifier"}))
		return
	}
	query.WillReturnRows(sqlmock.NewRows([]string{"payload_identifier", "hashed_payload_uuid", "mobileconfig_data", "installed"}).
		AddRow(retryTestProfileID, hash, []byte("<plist/>"), true))
}

// mockInsertRetryCommand is the INSERT the push path does for the re-sent command. The
// row is created with its attempt_count; there is no follow-up UPDATE.
func mockInsertRetryCommand(mockSpy sqlmock.Sqlmock, attempt int) {
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`INSERT INTO "commands"`).
		WithArgs(
			sqlmock.AnyArg(), // updated_at
			"retry-command-uuid",
			sqlmock.AnyArg(), // status
			retryTestUDID,
			"InstallProfile",
			sqlmock.AnyArg(), // payload
			sqlmock.AnyArg(), // queries
			retryTestProfileID,
			sqlmock.AnyArg(), // manifest_url
			retryTestHash,
			sqlmock.AnyArg(), // error_string
			attempt,          // attempt_count, carried in the payload so the INSERT has it
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()
}

func TestRetryErroredInstallProfile_ResendsDeviceProfile(t *testing.T) {
	setupRetryFlags(t, 2)
	waits := stubRetrySleep(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: retryTestUDID, SerialNumber: "C02TEST123"}

	mockFailedCommand(mockSpy, 0)
	mockNotOptedIntoDDM(mockSpy)
	mockDeviceProfile(mockSpy, retryTestHash)
	// PushProfiles: dedupe lookup, device lookup, enqueue
	mockCommandInQueueWithHash(mockSpy, retryTestUDID, "InstallProfile", retryTestProfileID, retryTestHash, false)
	mockGetDevice(mockSpy, retryTestUDID)
	mockInsertRetryCommand(mockSpy, 1)

	err := retryErroredInstallProfile(device, retryTestCmdUUID)

	require.NoError(t, err)
	assert.Equal(t, []time.Duration{1 * time.Second}, *waits, "first retry waits 1s")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRetryErroredInstallProfile_FallsBackToSharedProfile(t *testing.T) {
	setupRetryFlags(t, 2)
	waits := stubRetrySleep(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: retryTestUDID, SerialNumber: "C02TEST123"}

	mockFailedCommand(mockSpy, 1)
	mockNotOptedIntoDDM(mockSpy)
	mockDeviceProfile(mockSpy, "")
	mockSharedProfile(mockSpy, retryTestHash)
	// PushSharedProfiles: device-specific skip list, dedupe, device lookup, enqueue
	mockNoSkipProfileDevices(mockSpy, retryTestProfileID)
	mockCommandInQueueWithHash(mockSpy, retryTestUDID, "InstallProfile", retryTestProfileID, retryTestHash, false)
	mockGetDevice(mockSpy, retryTestUDID)
	mockInsertRetryCommand(mockSpy, 2)

	err := retryErroredInstallProfile(device, retryTestCmdUUID)

	require.NoError(t, err)
	assert.Equal(t, []time.Duration{2 * time.Second}, *waits, "second retry waits 2s")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRetryErroredInstallProfile_StopsAtLimit(t *testing.T) {
	waits := stubRetrySleep(t)
	defer func() { assert.Empty(t, *waits, "no backoff when the retry is skipped") }()
	setupRetryFlags(t, 2)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: retryTestUDID}

	// attempt_count already equals the limit: load the row and stop, no push.
	mockFailedCommand(mockSpy, 2)

	err := retryErroredInstallProfile(device, retryTestCmdUUID)

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRetryErroredInstallProfile_DisabledByFlag(t *testing.T) {
	waits := stubRetrySleep(t)
	defer func() { assert.Empty(t, *waits, "no backoff when the retry is skipped") }()
	setupRetryFlags(t, 0)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: retryTestUDID}

	// No DB access at all when retries are off.
	err := retryErroredInstallProfile(device, retryTestCmdUUID)

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRetryErroredInstallProfile_SkipsDDMDevice(t *testing.T) {
	waits := stubRetrySleep(t)
	defer func() { assert.Empty(t, *waits, "no backoff when the retry is skipped") }()
	setupRetryFlags(t, 2)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: retryTestUDID}

	mockFailedCommand(mockSpy, 0)
	mockSpy.ExpectQuery(`SELECT count\(\*\) FROM "ddm_opt_ins" WHERE device_ud_id = \$1`).
		WithArgs(retryTestUDID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	err := retryErroredInstallProfile(device, retryTestCmdUUID)

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRetryErroredInstallProfile_SkipsChangedContent(t *testing.T) {
	stubRetrySleep(t)
	setupRetryFlags(t, 2)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: retryTestUDID}

	mockFailedCommand(mockSpy, 0)
	mockNotOptedIntoDDM(mockSpy)
	// The assigned profile now has different content than the failed command carried.
	mockDeviceProfile(mockSpy, "hash-newer")

	err := retryErroredInstallProfile(device, retryTestCmdUUID)

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRetryErroredInstallProfile_SkipsUnassignedProfile(t *testing.T) {
	stubRetrySleep(t)
	setupRetryFlags(t, 2)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: retryTestUDID}

	mockFailedCommand(mockSpy, 0)
	mockNotOptedIntoDDM(mockSpy)
	mockDeviceProfile(mockSpy, "")
	mockSharedProfile(mockSpy, "")

	err := retryErroredInstallProfile(device, retryTestCmdUUID)

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRetryErroredInstallProfile_UnknownCommandIsNoop(t *testing.T) {
	setupRetryFlags(t, 2)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: retryTestUDID}

	mockSpy.ExpectQuery(`SELECT \* FROM "commands" WHERE device_ud_id = \$1 AND command_uuid = \$2`).
		WithArgs(retryTestUDID, retryTestCmdUUID).
		WillReturnRows(sqlmock.NewRows([]string{"command_uuid"}))

	err := retryErroredInstallProfile(device, retryTestCmdUUID)

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestErroredInstallProfiles_LatestResultPerIdentifier(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"identifier", "content_hash", "status", "attempt_count"}).
		AddRow("com.example.failed", "hash-a", "Error", 3).
		AddRow("com.example.recovered", "hash-b", "Acknowledged", 0).
		AddRow("com.example.pending", "hash-c", "", 1).
		AddRow("com.example.retried-via-ddm", "hash-d", commandStatusRetriedViaDDM, 0)
	mockSpy.ExpectQuery(`SELECT DISTINCT ON \(identifier\) identifier, content_hash, status, attempt_count FROM commands WHERE device_ud_id = \$1 AND request_type = \$2 ORDER BY identifier, updated_at DESC`).
		WithArgs(retryTestUDID, "InstallProfile").
		WillReturnRows(rows)

	errored, err := erroredInstallProfiles(retryTestUDID)

	require.NoError(t, err)
	assert.Equal(t, map[string]erroredInstall{"com.example.failed": {ContentHash: "hash-a", AttemptCount: 3}}, errored)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestMarkErroredInstallsRetriedViaDDM(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "commands" SET "status"=\$1,"updated_at"=\$2 WHERE device_ud_id = \$3 AND request_type = \$4 AND status = \$5 AND identifier IN \(\$6,\$7\)`).
		WithArgs(commandStatusRetriedViaDDM, sqlmock.AnyArg(), retryTestUDID, "InstallProfile", "Error", "com.example.a", "com.example.b").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mockSpy.ExpectCommit()

	err := markErroredInstallsRetriedViaDDM(retryTestUDID, []string{"com.example.a", "com.example.b"})

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestMarkErroredInstallsRetriedViaDDM_NothingToMark(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	require.NoError(t, markErroredInstallsRetriedViaDDM(retryTestUDID, nil))
	assert.NoError(t, mockSpy.ExpectationsWereMet(), "no query is issued for an empty list")
}

func TestLastInstallErrored(t *testing.T) {
	errored := map[string]erroredInstall{"com.example.failed": {ContentHash: "HASH-A", AttemptCount: 3}}
	current := ProfileForVerification{PayloadIdentifier: "com.example.failed", HashedPayloadUUID: "hash-a"}

	attempt, retry := lastInstallErrored(errored, current, 5)
	assert.True(t, retry, "latest install of the current content errored (case-insensitive) and is below the total")
	assert.Equal(t, 4, attempt, "the next command continues the counter")

	_, retry = lastInstallErrored(errored, current, 3)
	assert.False(t, retry, "the total retry limit has been reached for this content")

	_, retry = lastInstallErrored(errored, ProfileForVerification{PayloadIdentifier: "com.example.failed", HashedPayloadUUID: "hash-newer"}, 5)
	assert.False(t, retry, "the error was for older content")

	_, retry = lastInstallErrored(errored, ProfileForVerification{PayloadIdentifier: "com.example.other", HashedPayloadUUID: "hash-a"}, 5)
	assert.False(t, retry, "no errored install for this identifier")
}

// TestRetryErroredInstallProfile_SlowTierPushGetsNoFastChain: a command VerifyMDMProfiles
// sent is numbered above the fast limit, so when it fails the webhook leaves it to the
// next ProfileList.
func TestRetryErroredInstallProfile_SlowTierPushGetsNoFastChain(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	waits := stubRetrySleep(t)
	setupRetryFlags(t, 3)

	device := types.Device{UDID: retryTestUDID}
	mockFailedCommand(mockSpy, 4)

	require.NoError(t, retryErroredInstallProfile(device, retryTestCmdUUID))

	assert.Empty(t, *waits)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
