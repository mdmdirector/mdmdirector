package director

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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
func setupRetryFlags(t *testing.T, retries int) {
	t.Helper()
	setupDDMFlags(t, false, false)

	if flag.Lookup("install-profile-retries") == nil {
		flag.Int("install-profile-retries", 2, "retries")
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

// mockInsertRetryCommand is the INSERT the push path does for the re-sent command, plus
// the attempt_count bookkeeping the retry writes afterwards.
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
			sqlmock.AnyArg(), // attempt_count
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "commands" SET "attempt_count"=\$1,"updated_at"=\$2 WHERE command_uuid = \$3`).
		WithArgs(attempt, sqlmock.AnyArg(), "retry-command-uuid").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()
}

func TestRetryErroredInstallProfile_ResendsDeviceProfile(t *testing.T) {
	setupRetryFlags(t, 2)
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
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRetryErroredInstallProfile_FallsBackToSharedProfile(t *testing.T) {
	setupRetryFlags(t, 2)
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
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRetryErroredInstallProfile_StopsAtLimit(t *testing.T) {
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
