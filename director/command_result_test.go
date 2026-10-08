package director

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateCommand_UnknownCommandIsRecordedAsNoop: a result for a command mdmdirector
// has no row for (enqueued directly on the MDM server, or expired) updates nothing and
// is not an error, so the caller still processes the payload. Before, the existence
// check was a Where() with no finisher whose Error was always nil, so this path was
// indistinguishable from a recorded result.
func TestUpdateCommand_UnknownCommandIsRecordedAsNoop(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	setupRetryFlags(t, 2)
	waits := stubRetrySleep(t)

	mockSpy.ExpectQuery(`SELECT "request_type" FROM "commands" WHERE command_uuid = \$1`).
		WithArgs("unknown-uuid").
		WillReturnError(errors.New("record not found"))
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "commands" SET "updated_at"=\$1,"status"=\$2,"error_string"=\$3 WHERE device_ud_id = \$4 AND command_uuid = \$5`).
		WithArgs(sqlmock.AnyArg(), "Error", "<plist/>", retryTestUDID, "unknown-uuid").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mockSpy.ExpectCommit()

	err := UpdateCommand(
		&types.AcknowledgeEvent{CommandUUID: "unknown-uuid", Status: "Error", RawPayload: []byte("<plist/>")},
		types.Device{UDID: retryTestUDID},
		map[string]interface{}{},
	)

	require.NoError(t, err)
	assert.Empty(t, *waits, "no row, so nothing to retry")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestUpdateCommand_WriteFailureIsReturned: a failed status write surfaces instead of
// being swallowed.
func TestUpdateCommand_WriteFailureIsReturned(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "commands" SET`).WillReturnError(errors.New("connection reset"))
	mockSpy.ExpectRollback()

	err := UpdateCommand(
		&types.AcknowledgeEvent{CommandUUID: "cmd", Status: "Acknowledged"},
		types.Device{UDID: retryTestUDID},
		map[string]interface{}{"ProfileList": []interface{}{}},
	)

	require.Error(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestSendCommand_NanoMDM_RecordFailureIsReturned: the command has already been enqueued
// on the MDM server when the row insert fails. The error is returned so the caller logs
// it, instead of leaving a command in flight that no result can be matched back to.
func TestSendCommand_NanoMDM_RecordFailureIsReturned(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := mdm.APIResponse{
			CommandUUID: "test-command-uuid-123",
			RequestType: "DeviceInformation",
			Status:      map[string]mdm.EnrollmentStatus{"test-udid-123": {PushResult: "success"}},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	mockGetDevice(mockSpy, "test-udid-123")
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`INSERT INTO "commands"`).WillReturnError(errors.New("disk full"))
	mockSpy.ExpectRollback()

	command, err := sendCommandWithClient(nanoClient, types.CommandPayload{UDID: "test-udid-123", RequestType: "DeviceInformation"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "record sent command test-command-uuid-123")
	assert.Equal(t, "test-command-uuid-123", command.CommandUUID, "the enqueued UUID is still returned for logging")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestGetErrorCommands_FiltersInTheQuery: the handler used to Find() the whole table and
// only then apply the status filter on a second query.
func TestGetErrorCommands_FiltersInTheQuery(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(`SELECT \* FROM "commands" WHERE status = \$1`).
		WithArgs("Error").
		WillReturnRows(sqlmock.NewRows([]string{"command_uuid", "status"}).AddRow("cmd-1", "Error"))

	rr := httptest.NewRecorder()
	GetErrorCommands(rr, httptest.NewRequest(http.MethodGet, "/command/error", nil))

	assert.Equal(t, http.StatusOK, rr.Code)
	var got []types.Command
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "cmd-1", got[0].CommandUUID)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestUpdateCommand_EmptyUDIDIsRejected: a malformed acknowledge event with no device
// UDID is returned as an error before any query runs, instead of matching nothing and
// being logged as an unknown command.
func TestUpdateCommand_EmptyUDIDIsRejected(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	err := UpdateCommand(
		&types.AcknowledgeEvent{CommandUUID: "cmd", Status: "Acknowledged"},
		types.Device{},
		map[string]interface{}{},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "without a device UDID")
	assert.NoError(t, mockSpy.ExpectationsWereMet(), "no SQL is issued")
}

// microMDMAnswering points the MicroMDM path at a stub that answers every enqueue with
// the given status and JSON body.
func microMDMAnswering(t *testing.T, status int, body string) {
	t.Helper()
	setupRetryFlags(t, 2) // registers mdm-server-type, micromdmurl, micromdmapikey
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	setFlag(t, "micromdmurl", server.URL)
}

// TestSendCommand_MicroMDM_ErrorStatusRecordsNothing: a non-2xx answer with a decodable
// body used to produce a row with an empty command UUID. Now it is an error and no row.
func TestSendCommand_MicroMDM_ErrorStatusRecordsNothing(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	microMDMAnswering(t, http.StatusUnauthorized, `{"error":"bad api key"}`)
	mockGetDevice(mockSpy, "test-udid-123")

	_, err := SendCommand(types.CommandPayload{UDID: "test-udid-123", RequestType: "DeviceInformation"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.NoError(t, mockSpy.ExpectationsWereMet(), "no INSERT")
}

// TestSendCommand_MicroMDM_EmptyUUIDRecordsNothing: a 2xx with no command UUID is not a
// command that exists, so nothing is recorded.
func TestSendCommand_MicroMDM_EmptyUUIDRecordsNothing(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	microMDMAnswering(t, http.StatusOK, `{"payload":{}}`)
	mockGetDevice(mockSpy, "test-udid-123")

	_, err := SendCommand(types.CommandPayload{UDID: "test-udid-123", RequestType: "DeviceInformation"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no command UUID")
	assert.NoError(t, mockSpy.ExpectationsWereMet(), "no INSERT")
}
