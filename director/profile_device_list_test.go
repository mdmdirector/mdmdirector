package director

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func deviceListTestProfile() types.DeviceProfile {
	return types.DeviceProfile{
		DeviceUDID:        "1234-5678-123456",
		PayloadIdentifier: "com.example.profile",
		HashedPayloadUUID: "HASH-NEW",
	}
}

func expectDeviceHasProfileList(mockSpy sqlmock.Sqlmock, hasList bool) {
	rows := sqlmock.NewRows([]string{"id", "device_ud_id", "payload_identifier", "payload_uuid"})
	if hasList {
		rows.AddRow("c0ffee00-0000-0000-0000-000000000001", "1234-5678-123456", "com.example.enroll", "ENROLL")
	}
	mockSpy.ExpectQuery(`SELECT \* FROM "profile_lists" WHERE device_ud_id = \$1 LIMIT 1`).
		WithArgs("1234-5678-123456").
		WillReturnRows(rows)
}

func expectProfileListEntry(mockSpy sqlmock.Sqlmock, payloadUUID string) {
	rows := sqlmock.NewRows([]string{"id", "device_ud_id", "payload_identifier", "payload_uuid"})
	if payloadUUID != "" {
		rows.AddRow("c0ffee00-0000-0000-0000-000000000002", "1234-5678-123456", "com.example.profile", payloadUUID)
	}
	mockSpy.ExpectQuery(`SELECT \* FROM "profile_lists" WHERE device_ud_id = \$1 AND payload_identifier = \$2`).
		WithArgs("1234-5678-123456", "com.example.profile").
		WillReturnRows(rows)
}

func expectRejectedInstalls(mockSpy sqlmock.Sqlmock, count int) {
	mockSpy.ExpectQuery(`SELECT count\(\*\) FROM "commands" WHERE device_ud_id = \$1 AND request_type = \$2 AND identifier = \$3 AND content_hash = \$4 AND status = \$5`).
		WithArgs("1234-5678-123456", "InstallProfile", "com.example.profile", "HASH-NEW", "Error").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
}

func TestProfileMissingFromDevice_NoProfileListYet(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: "1234-5678-123456"}

	expectDeviceHasProfileList(mockSpy, false)

	missing, err := profileMissingFromDevice(device, deviceListTestProfile())
	require.NoError(t, err)
	assert.False(t, missing, "with no ProfileList there is nothing to compare against")
	require.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestProfileMissingFromDevice_ListedWithSameContent(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: "1234-5678-123456"}

	expectDeviceHasProfileList(mockSpy, true)
	expectProfileListEntry(mockSpy, "hash-new")

	missing, err := profileMissingFromDevice(device, deviceListTestProfile())
	require.NoError(t, err)
	assert.False(t, missing, "PayloadUUID match is case-insensitive")
	require.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestProfileMissingFromDevice_NotListed(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: "1234-5678-123456"}

	expectDeviceHasProfileList(mockSpy, true)
	expectProfileListEntry(mockSpy, "")
	expectRejectedInstalls(mockSpy, 0)

	missing, err := profileMissingFromDevice(device, deviceListTestProfile())
	require.NoError(t, err)
	assert.True(t, missing)
	require.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestProfileMissingFromDevice_OldContentListed(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: "1234-5678-123456"}

	expectDeviceHasProfileList(mockSpy, true)
	expectProfileListEntry(mockSpy, "HASH-OLD")
	expectRejectedInstalls(mockSpy, 0)

	missing, err := profileMissingFromDevice(device, deviceListTestProfile())
	require.NoError(t, err)
	assert.True(t, missing, "device still has the previous version")
	require.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestProfileMissingFromDevice_DeviceRejectedThisContent(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: "1234-5678-123456"}

	expectDeviceHasProfileList(mockSpy, true)
	expectProfileListEntry(mockSpy, "")
	expectRejectedInstalls(mockSpy, 1)

	missing, err := profileMissingFromDevice(device, deviceListTestProfile())
	require.NoError(t, err)
	assert.False(t, missing, "a rejected install is left to VerifyMDMProfiles, not retried on every POST")
	require.NoError(t, mockSpy.ExpectationsWereMet())
}

func expectAckedCommand(mockSpy sqlmock.Sqlmock, requestType, identifier, contentHash string) {
	rows := sqlmock.NewRows([]string{"request_type", "identifier", "content_hash"})
	if requestType != "" {
		rows.AddRow(requestType, identifier, contentHash)
	}
	mockSpy.ExpectQuery(`SELECT "request_type","identifier","content_hash" FROM "commands" WHERE device_ud_id = \$1 AND command_uuid = \$2`).
		WithArgs("1234-5678-123456", "cmd-1").
		WillReturnRows(rows)
}

func TestRecordProfileAck_InstallProfileReplacesEntry(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: "1234-5678-123456"}

	expectAckedCommand(mockSpy, "InstallProfile", "com.example.profile", "HASH-NEW")
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`DELETE FROM "profile_lists" WHERE device_ud_id = \$1 AND payload_identifier = \$2`).
		WithArgs("1234-5678-123456", "com.example.profile").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectQuery(`INSERT INTO "profile_lists"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("c0ffee00-0000-0000-0000-000000000003"))
	mockSpy.ExpectCommit()

	require.NoError(t, recordProfileAck(device, "cmd-1"))
	require.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRecordProfileAck_RemoveProfileDeletesEntry(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: "1234-5678-123456"}

	expectAckedCommand(mockSpy, "RemoveProfile", "com.example.profile", "")
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`DELETE FROM "profile_lists" WHERE device_ud_id = \$1 AND payload_identifier = \$2`).
		WithArgs("1234-5678-123456", "com.example.profile").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	require.NoError(t, recordProfileAck(device, "cmd-1"))
	require.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestRecordProfileAck_IgnoresOtherCommands(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: "1234-5678-123456"}

	// Not found, an install with no content hash (enrollment profile), and a non-profile command.
	expectAckedCommand(mockSpy, "", "", "")
	expectAckedCommand(mockSpy, "InstallProfile", "com.example.enroll", "")
	expectAckedCommand(mockSpy, "DeviceInformation", "", "")

	for i := 0; i < 3; i++ {
		require.NoError(t, recordProfileAck(device, "cmd-1"))
	}
	require.NoError(t, mockSpy.ExpectationsWereMet())
}
