package director

import (
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDeviceRow is a device row with the state an already-enrolled device has, so the
// webhook neither runs initial tasks nor sends DeviceConfigured.
func mockDeviceRow() *sqlmock.Rows {
	udid := retryTestUDID
	return sqlmock.NewRows([]string{"ud_id", "serial_number", "initial_tasks_run", "token_update_recieved", "awaiting_configuration", "build_version"}).
		AddRow(udid, "C02TEST123", true, true, false, "")
}

// mockDeviceLookup is the single SELECT a First(&device) issues.
func mockDeviceLookup(mockSpy sqlmock.Sqlmock) {
	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1 ORDER BY "devices"\."ud_id" LIMIT 1`).
		WithArgs(retryTestUDID).WillReturnRows(mockDeviceRow())
}

func setupWebhookFlags(t *testing.T) {
	t.Helper()
	if flag.Lookup("push-new-build") == nil {
		flag.Bool("push-new-build", false, "push on new build")
	}
	require.NoError(t, flag.Set("push-new-build", "false"))
}

// TestHandleAcknowledgeEvent_ErrorAckRetriesInstallProfile drives the webhook's
// acknowledge path with the Error response a device sends for a failed InstallProfile and
// checks that the failed command is recorded and re-sent in the same request.
func TestHandleAcknowledgeEvent_ErrorAckRetriesInstallProfile(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	setupRetryFlags(t, 2)
	setupWebhookFlags(t)
	waits := stubRetrySleep(t)

	rawPayload := []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
	<key>CommandUUID</key><string>%s</string>
	<key>Status</key><string>Error</string>
	<key>UDID</key><string>%s</string>
	<key>ErrorChain</key><array><dict>
		<key>ErrorCode</key><integer>134030</integer>
		<key>ErrorDomain</key><string>CPProfileManager</string>
	</dict></array>
</dict></plist>`, retryTestCmdUUID, retryTestUDID))

	// previousBuildVersion -> GetDevice
	mockDeviceLookup(mockSpy)
	// UpdateDevice: load the row, then Assign+FirstOrCreate on it
	mockDeviceLookup(mockSpy)
	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1 AND "devices"\."ud_id" = \$2`).
		WithArgs(retryTestUDID, retryTestUDID).WillReturnRows(mockDeviceRow())
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	// UpdateCommand: the payload carries no response key, so the type comes from the row
	mockSpy.ExpectQuery(`SELECT "request_type" FROM "commands" WHERE command_uuid = \$1 ORDER BY "commands"\."command_uuid" LIMIT 1`).
		WithArgs(retryTestCmdUUID).
		WillReturnRows(sqlmock.NewRows([]string{"request_type"}).AddRow("InstallProfile"))
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "commands" SET "updated_at"=\$1,"status"=\$2,"error_string"=\$3 WHERE device_ud_id = \$4 AND command_uuid = \$5`).
		WithArgs(sqlmock.AnyArg(), "Error", string(rawPayload), retryTestUDID, retryTestCmdUUID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	// retryErroredInstallProfile
	mockFailedCommand(mockSpy, 0)
	mockNotOptedIntoDDM(mockSpy)
	mockDeviceProfile(mockSpy, retryTestHash)
	mockCommandInQueueWithHash(mockSpy, retryTestUDID, "InstallProfile", retryTestProfileID, retryTestHash, false)
	mockGetDevice(mockSpy, retryTestUDID)
	mockInsertRetryCommand(mockSpy, 1)

	err := handleAcknowledgeEvent(&types.AcknowledgeEvent{
		UDID:        retryTestUDID,
		CommandUUID: retryTestCmdUUID,
		Status:      "Error",
		RawPayload:  rawPayload,
	})

	require.NoError(t, err)
	assert.Equal(t, []time.Duration{time.Second}, *waits, "first retry waits 1s")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestHandleAcknowledgeEvent_AcknowledgedAckDoesNotRetry is the control: a successful
// InstallProfile result records the ack and never reaches the retry path.
func TestHandleAcknowledgeEvent_AcknowledgedAckDoesNotRetry(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	setupRetryFlags(t, 2)
	setupWebhookFlags(t)
	waits := stubRetrySleep(t)

	rawPayload := []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
	<key>CommandUUID</key><string>%s</string>
	<key>Status</key><string>Acknowledged</string>
	<key>UDID</key><string>%s</string>
</dict></plist>`, retryTestCmdUUID, retryTestUDID))

	mockDeviceLookup(mockSpy)
	mockDeviceLookup(mockSpy)
	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1 AND "devices"\."ud_id" = \$2`).
		WithArgs(retryTestUDID, retryTestUDID).WillReturnRows(mockDeviceRow())
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	mockSpy.ExpectQuery(`SELECT "request_type" FROM "commands" WHERE command_uuid = \$1`).
		WithArgs(retryTestCmdUUID).
		WillReturnRows(sqlmock.NewRows([]string{"request_type"}).AddRow("InstallProfile"))
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "commands" SET "updated_at"=\$1,"status"=\$2,"error_string"=\$3 WHERE device_ud_id = \$4 AND command_uuid = \$5`).
		WithArgs(sqlmock.AnyArg(), "Acknowledged", "", retryTestUDID, retryTestCmdUUID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()
	// recordProfileAck loads the command; an unknown one is a no-op
	mockSpy.ExpectQuery(`SELECT "request_type","identifier","content_hash" FROM "commands" WHERE device_ud_id = \$1 AND command_uuid = \$2`).
		WithArgs(retryTestUDID, retryTestCmdUUID).
		WillReturnRows(sqlmock.NewRows([]string{"request_type"}))

	err := handleAcknowledgeEvent(&types.AcknowledgeEvent{
		UDID:        retryTestUDID,
		CommandUUID: retryTestCmdUUID,
		Status:      "Acknowledged",
		RawPayload:  rawPayload,
	})

	require.NoError(t, err)
	assert.Empty(t, *waits, "no retry, so no backoff")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
