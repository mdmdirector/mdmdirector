package director

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCommandInQueue sets up the DB expectation for the CommandInQueue SELECT and
// controls whether it reports a pending command.
func mockCommandInQueue(mockSpy sqlmock.Sqlmock, udid, requestType, identifier string, found bool) {
	mockCommandInQueueWithHash(mockSpy, udid, requestType, identifier, "", found)
}

func mockCommandInQueueWithHash(mockSpy sqlmock.Sqlmock, udid, requestType, identifier, contentHash string, found bool) {
	query := mockSpy.ExpectQuery(
		`SELECT \* FROM "commands" WHERE \(device_ud_id = \$1 AND request_type = \$2 AND identifier = \$3 AND COALESCE\(content_hash, ''\) = \$4\) AND \(status = \$5 OR status = \$6\) ORDER BY "commands"\."command_uuid" LIMIT 1`,
	).WithArgs(udid, requestType, identifier, contentHash, "", "NotNow")

	if found {
		rows := sqlmock.NewRows([]string{"command_uuid", "status", "device_ud_id", "request_type", "identifier"}).
			AddRow("existing-command-uuid", "", udid, requestType, identifier)
		query.WillReturnRows(rows)
		return
	}
	query.WillReturnRows(sqlmock.NewRows([]string{"command_uuid"}))
}

// mockNoSkipProfileDevices sets up the DB expectation for the device-specific-profile
// lookup that PushSharedProfiles/DeleteSharedProfiles run before iterating devices.
func mockNoSkipProfileDevices(mockSpy sqlmock.Sqlmock, identifier string) {
	mockSpy.ExpectQuery(`SELECT "device_ud_id" FROM "device_profiles" WHERE payload_identifier = \$1`).
		WithArgs(identifier).
		WillReturnRows(sqlmock.NewRows([]string{"device_ud_id"}))
}

func TestCommandInQueue_Found(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockCommandInQueue(mockSpy, "test-udid", "InstallProfile", "com.example.foo", true)

	inQueue, err := CommandInQueue(types.Device{UDID: "test-udid"}, "InstallProfile", "com.example.foo")

	require.NoError(t, err)
	assert.True(t, inQueue)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestCommandInQueue_NotFound(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockCommandInQueue(mockSpy, "test-udid", "InstallProfile", "com.example.foo", false)

	inQueue, err := CommandInQueue(types.Device{UDID: "test-udid"}, "InstallProfile", "com.example.foo")

	require.NoError(t, err)
	assert.False(t, inQueue)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestCommandInQueue_EmptyIdentifierForNonProfileCommands(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockCommandInQueue(mockSpy, "test-udid", "SecurityInfo", "", true)

	inQueue, err := CommandInQueue(types.Device{UDID: "test-udid"}, "SecurityInfo", "")

	require.NoError(t, err)
	assert.True(t, inQueue)
}

func TestCommandInQueue_DBError(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(`SELECT \* FROM "commands"`).
		WillReturnError(errors.New("database has gone away"))

	inQueue, err := CommandInQueue(types.Device{UDID: "test-udid"}, "InstallProfile", "com.example.foo")

	require.Error(t, err)
	assert.False(t, inQueue)
	assert.Contains(t, err.Error(), "database has gone away")
}

// --- Request* helpers: each must skip SendCommand entirely when a matching command is
// already pending. If the skip check didn't run (or ran but didn't short-circuit), the
// unmocked GetDevice/SendCommand queries that would follow return an error here, which
// these tests would surface as a non-nil err.

func TestRequestCertificateList_SkipsWhenAlreadyQueued(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	device := types.Device{UDID: "test-udid", SerialNumber: "C02TEST123"}
	mockCommandInQueue(mockSpy, device.UDID, "CertificateList", "", true)

	err := RequestCertificateList(device)

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRequestSecurityInfo_SkipsWhenAlreadyQueued(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	device := types.Device{UDID: "test-udid", SerialNumber: "C02TEST123"}
	mockCommandInQueue(mockSpy, device.UDID, "SecurityInfo", "", true)

	err := RequestSecurityInfo(device)

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRequestDeviceInformation_SkipsWhenAlreadyQueued(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	device := types.Device{UDID: "test-udid", SerialNumber: "C02TEST123"}
	mockCommandInQueue(mockSpy, device.UDID, "DeviceInformation", "", true)

	err := RequestDeviceInformation(device)

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRequestProfileList_SkipsWhenAlreadyQueued(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	device := types.Device{UDID: "test-udid", SerialNumber: "C02TEST123"}
	mockCommandInQueue(mockSpy, device.UDID, "ProfileList", "", true)

	err := RequestProfileList(device)

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// --- Profile push/delete helpers: same skip guarantee, keyed by profile identifier
// rather than empty string, since multiple distinct profiles must not dedupe against
// each other.

func TestPushProfiles_SkipsWhenAlreadyQueued(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	device := types.Device{UDID: "test-udid", SerialNumber: "C02TEST123"}
	profile := types.DeviceProfile{PayloadIdentifier: "com.example.profile"}
	mockCommandInQueue(mockSpy, device.UDID, "InstallProfile", profile.PayloadIdentifier, true)

	commands, err := PushProfiles([]types.Device{device}, []types.DeviceProfile{profile}, false)

	require.NoError(t, err)
	assert.Empty(t, commands)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestPushSharedProfiles_SkipsWhenAlreadyQueued(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	device := types.Device{UDID: "test-udid", SerialNumber: "C02TEST123"}
	profile := types.SharedProfile{PayloadIdentifier: "com.example.shared"}
	mockNoSkipProfileDevices(mockSpy, profile.PayloadIdentifier)
	mockCommandInQueue(mockSpy, device.UDID, "InstallProfile", profile.PayloadIdentifier, true)

	commands, err := PushSharedProfiles([]types.Device{device}, []types.SharedProfile{profile}, false)

	require.NoError(t, err)
	assert.Empty(t, commands)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestDeleteDeviceProfiles_SkipsWhenAlreadyQueued(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	device := types.Device{UDID: "test-udid", SerialNumber: "C02TEST123"}
	profile := types.DeviceProfile{PayloadIdentifier: "com.example.profile"}
	mockCommandInQueue(mockSpy, device.UDID, "RemoveProfile", profile.PayloadIdentifier, true)

	commands, err := DeleteDeviceProfiles([]types.Device{device}, []types.DeviceProfile{profile}, false)

	require.NoError(t, err)
	assert.Empty(t, commands)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestDeleteSharedProfiles_SkipsWhenAlreadyQueued(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	device := types.Device{UDID: "test-udid", SerialNumber: "C02TEST123"}
	profile := types.SharedProfile{PayloadIdentifier: "com.example.shared"}
	mockNoSkipProfileDevices(mockSpy, profile.PayloadIdentifier)
	mockCommandInQueue(mockSpy, device.UDID, "RemoveProfile", profile.PayloadIdentifier, true)

	commands, err := DeleteSharedProfiles([]types.Device{device}, []types.SharedProfile{profile}, false)

	require.NoError(t, err)
	assert.Empty(t, commands)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// --- Not-in-queue path: confirms the dedup check doesn't swallow a legitimate send.
// SendCommand itself (micromdm and nanomdm paths) already has its own coverage in
// command_nanomdm_test.go / command_test.go; here we only need to prove that a "not
// found" CommandInQueue result falls through into it rather than skipping, using the
// micromdm HTTP path since it needs no global client singleton setup.

func TestRequestSecurityInfo_SendsWhenNotQueued(t *testing.T) {
	// Ensure we use the microMDM code path (flag may be set to nanomdm by other tests).
	if flag.Lookup("mdm-server-type") != nil {
		_ = flag.Set("mdm-server-type", "micromdm")
	}
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	device := types.Device{UDID: "test-udid-123", SerialNumber: "C02TEST123"}
	mockCommandInQueue(mockSpy, device.UDID, "SecurityInfo", "", false)
	mockGetDevice(mockSpy, device.UDID)
	mockCreateCommand(mockSpy)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.CommandResponse{}
		resp.Payload.CommandUUID = "new-command-uuid"
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	if flag.Lookup("micromdmurl") == nil {
		flag.String("micromdmurl", "", "MicroMDM Server URL")
	}
	if flag.Lookup("micromdmapikey") == nil {
		flag.String("micromdmapikey", "", "MicroMDM Server API Key")
	}
	if flag.Lookup("prometheus") == nil {
		flag.Bool("prometheus", false, "Enable prometheus metrics")
	}
	require.NoError(t, flag.Set("micromdmurl", server.URL))
	require.NoError(t, flag.Set("micromdmapikey", "test-key"))

	err := RequestSecurityInfo(device)

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// --- InstallProfile content-hash dedup ---

func TestInstallProfileInQueue_SameContentDedupes(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockCommandInQueueWithHash(mockSpy, "test-udid", "InstallProfile", "com.example.profile", "hash-a", true)

	inQueue, err := InstallProfileInQueue(types.Device{UDID: "test-udid"}, "com.example.profile", "hash-a")

	require.NoError(t, err)
	assert.True(t, inQueue)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestPushProfiles_ReenqueuesWhenQueuedContentIsStale covers the offline-device race: an
// InstallProfile for the same identifier is pending, but with an older content hash. The
// dedup lookup is filtered by the profile's current hash, so the stale row doesn't match
// and the fresh content is actually enqueued (with the new hash persisted), instead of
// the device later receiving only the outdated payload from NanoMDM/MicroMDM's queue.
func TestPushProfiles_ReenqueuesWhenQueuedContentIsStale(t *testing.T) {
	if flag.Lookup("mdm-server-type") != nil {
		_ = flag.Set("mdm-server-type", "micromdm")
	}
	if flag.Lookup("sign") == nil {
		flag.Bool("sign", false, "sign profiles")
	}
	require.NoError(t, flag.Set("sign", "false"))
	if flag.Lookup("micromdmurl") == nil {
		flag.String("micromdmurl", "", "MicroMDM Server URL")
	}
	if flag.Lookup("micromdmapikey") == nil {
		flag.String("micromdmapikey", "", "MicroMDM Server API Key")
	}
	if flag.Lookup("prometheus") == nil {
		flag.Bool("prometheus", false, "Enable prometheus metrics")
	}

	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	device := types.Device{UDID: "test-udid-123", SerialNumber: "C02TEST123"}
	profile := types.DeviceProfile{
		PayloadIdentifier: "com.example.profile",
		HashedPayloadUUID: "hash-new",
		MobileconfigData:  []byte("<plist/>"),
	}

	mockCommandInQueueWithHash(mockSpy, device.UDID, "InstallProfile", profile.PayloadIdentifier, "hash-new", false)
	mockGetDevice(mockSpy, device.UDID)
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`INSERT INTO "commands"`).
		WithArgs(
			sqlmock.AnyArg(), // updated_at
			"new-command-uuid",
			sqlmock.AnyArg(), // status
			device.UDID,
			"InstallProfile",
			sqlmock.AnyArg(), // payload
			sqlmock.AnyArg(), // queries
			profile.PayloadIdentifier,
			sqlmock.AnyArg(), // manifest_url
			"hash-new",
			sqlmock.AnyArg(), // error_string
			sqlmock.AnyArg(), // attempt_count
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.CommandResponse{}
		resp.Payload.CommandUUID = "new-command-uuid"
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	require.NoError(t, flag.Set("micromdmurl", server.URL))
	require.NoError(t, flag.Set("micromdmapikey", "test-key"))

	commands, err := PushProfiles([]types.Device{device}, []types.DeviceProfile{profile}, false)

	require.NoError(t, err)
	require.Len(t, commands, 1)
	assert.Equal(t, "hash-new", commands[0].ContentHash)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
