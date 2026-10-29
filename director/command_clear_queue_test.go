package director

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clearedFirst matches the UDID argument of the pending-row DELETE, and only once
// NanoMDM has been asked to clear that device's queue. It pins the order: rows go only
// after the MDM server's queue has.
type clearedFirst struct {
	udid    string
	cleared func(udid string) bool
}

func (c clearedFirst) Match(v driver.Value) bool {
	return v == c.udid && c.cleared(c.udid)
}

// mockDeletePendingRows expects the DELETE of udid's pending command rows.
func mockDeletePendingRows(mockSpy sqlmock.Sqlmock, udid interface{}, deleted int64) {
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^DELETE FROM "commands" WHERE status IN \(\$1,\$2\) AND device_ud_id = \$3$`).
		WithArgs("", "NotNow", udid).
		WillReturnResult(sqlmock.NewResult(0, deleted))
	mockSpy.ExpectCommit()
}

// fakeNanoMDMQueue records which enrollment IDs NanoMDM was asked to clear, and fails
// the clear for the IDs in fail.
type fakeNanoMDMQueue struct {
	mu      sync.Mutex
	cleared []string
	fail    map[string]bool
}

func (f *fakeNanoMDMQueue) handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete || !strings.HasPrefix(r.URL.Path, "/v1/queue/") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/queue/")
	if f.fail[id] {
		// 404 isn't retried, so the test doesn't wait on backoff.
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(mdm.QueueDeleteResponse{Error: "enrollment not found"})
		return
	}
	f.mu.Lock()
	f.cleared = append(f.cleared, id)
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeNanoMDMQueue) wasCleared(udid string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range f.cleared {
		if id == udid {
			return true
		}
	}
	return false
}

func TestClearCommandQueue_NanoMDM_DeletesPendingRowsAfterClear(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	fake := &fakeNanoMDMQueue{}
	nanoClient := newMockNanoMDMServer(t, fake.handler)
	mockDeletePendingRows(mockSpy, clearedFirst{udid: "test-udid-123", cleared: fake.wasCleared}, 3)

	err := clearCommandQueueWithClient(nanoClient, types.Device{UDID: "test-udid-123"})

	require.NoError(t, err)
	assert.Equal(t, []string{"test-udid-123"}, fake.cleared)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// If NanoMDM didn't clear the queue its commands may still be delivered, so the rows that
// track them must stay.
func TestClearCommandQueue_NanoMDM_FailureLeavesRows(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	fake := &fakeNanoMDMQueue{fail: map[string]bool{"test-udid-123": true}}
	nanoClient := newMockNanoMDMServer(t, fake.handler)

	err := clearCommandQueueWithClient(nanoClient, types.Device{UDID: "test-udid-123"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "clearCommandQueue via NanoMDM")
	// No DELETE was expected; any statement would have failed the call above.
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestClearCommandQueue_DeleteRowsError(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	fake := &fakeNanoMDMQueue{}
	nanoClient := newMockNanoMDMServer(t, fake.handler)
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`DELETE FROM "commands"`).WillReturnError(assert.AnError)
	mockSpy.ExpectRollback()

	err := clearCommandQueueWithClient(nanoClient, types.Device{UDID: "test-udid-123"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete pending commands for test-udid-123")
}

// After a clear the next push must enqueue the profile again: the dedupe check finds no
// pending row instead of skipping the profile as "already in queue".
func TestClearCommandQueue_ThenPushProfilesRequeues(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	fake := &fakeNanoMDMQueue{}
	var enqueued int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/enqueue/") {
			enqueued++
			_ = json.NewEncoder(w).Encode(mdm.APIResponse{
				CommandUUID: "new-command-uuid",
				RequestType: "InstallProfile",
				Status:      map[string]mdm.EnrollmentStatus{"test-udid": {PushResult: "success"}},
			})
			return
		}
		fake.handler(w, r)
	}))
	t.Cleanup(server.Close)
	mdm.InitClient(server.URL, "test-api-key")

	device := types.Device{UDID: "test-udid", SerialNumber: "C02TEST123"}
	profile := types.DeviceProfile{PayloadIdentifier: "com.example.profile", HashedPayloadUUID: "hash-1"}

	mockDeletePendingRows(mockSpy, clearedFirst{udid: device.UDID, cleared: fake.wasCleared}, 1)
	mockCommandInQueueWithHash(mockSpy, device.UDID, "InstallProfile", profile.PayloadIdentifier, profile.HashedPayloadUUID, false)
	mockGetDevice(mockSpy, device.UDID)
	mockCreateCommand(mockSpy)

	require.NoError(t, clearCommandQueue(device))
	commands, err := PushProfiles([]types.Device{device}, []types.DeviceProfile{profile}, false)

	require.NoError(t, err)
	require.Len(t, commands, 1)
	assert.Equal(t, "new-command-uuid", commands[0].CommandUUID)
	assert.Equal(t, 1, enqueued)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func postDeviceCommand(t *testing.T, command string, payload types.DeviceCommandPayload) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/device/command/"+command, bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"command": command})
	rec := httptest.NewRecorder()
	PostDeviceCommandHandler(rec, req)
	return rec
}

// Every device in the payload is cleared, not just the first.
func TestPostDeviceCommandHandler_ClearQueue_AllDevices(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	fake := &fakeNanoMDMQueue{}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(server.Close)
	mdm.InitClient(server.URL, "test-api-key")

	udids := []string{"udid-1", "udid-2", "udid-3"}
	for _, udid := range udids {
		mockGetDevice(mockSpy, udid)
	}
	for _, udid := range udids {
		mockDeletePendingRows(mockSpy, udid, 2)
	}

	rec := postDeviceCommand(t, "clear_queue", types.DeviceCommandPayload{DeviceUDIDs: udids})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, udids, fake.cleared)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// A device whose clear fails, or that doesn't exist, doesn't stop the others, and the
// request answers one error naming them.
func TestPostDeviceCommandHandler_ClearQueue_PartialFailure(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	fake := &fakeNanoMDMQueue{fail: map[string]bool{"udid-2": true}}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(server.Close)
	mdm.InitClient(server.URL, "test-api-key")

	mockGetDevice(mockSpy, "udid-1")
	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1`).
		WithArgs("missing-udid").
		WillReturnRows(sqlmock.NewRows([]string{"ud_id"}))
	mockGetDevice(mockSpy, "udid-2")
	mockGetDevice(mockSpy, "udid-3")
	mockDeletePendingRows(mockSpy, "udid-1", 1)
	// udid-2: NanoMDM fails, its rows stay
	mockDeletePendingRows(mockSpy, "udid-3", 1)

	rec := postDeviceCommand(t, "clear_queue", types.DeviceCommandPayload{DeviceUDIDs: []string{"udid-1", "missing-udid", "udid-2", "udid-3"}})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "clear_queue failed for: missing-udid, udid-2\n", rec.Body.String())
	assert.Equal(t, []string{"udid-1", "udid-3"}, fake.cleared)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestPostDeviceCommandHandler_BadBody(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/device/command/clear_queue", strings.NewReader("{not json"))
	req = mux.SetURLVars(req, map[string]string{"command": "clear_queue"})
	rec := httptest.NewRecorder()
	PostDeviceCommandHandler(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// setupMicroMDMClear points the MicroMDM code path at a server answering the queue DELETE
// with status.
func setupMicroMDMClear(t *testing.T, status int) *string {
	t.Helper()
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		gotPath = r.URL.Path
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	setupNanoMDMFlag(t)
	previous := flag.Lookup("mdm-server-type").Value.String()
	require.NoError(t, flag.Set("mdm-server-type", "micromdm"))
	t.Cleanup(func() { _ = flag.Set("mdm-server-type", previous) })
	if flag.Lookup("micromdmurl") == nil {
		flag.String("micromdmurl", server.URL, "MicroMDM Server URL")
	}
	require.NoError(t, flag.Set("micromdmurl", server.URL))
	if flag.Lookup("micromdmapikey") == nil {
		flag.String("micromdmapikey", "", "MicroMDM Server API Key")
	}
	return &gotPath
}

func TestClearCommandQueue_MicroMDM_DeletesPendingRows(t *testing.T) {
	gotPath := setupMicroMDMClear(t, http.StatusOK)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	mockDeletePendingRows(mockSpy, "test-udid", 2)

	err := clearCommandQueue(types.Device{UDID: "test-udid"})

	require.NoError(t, err)
	assert.Equal(t, "/v1/commands/test-udid", *gotPath)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestClearCommandQueue_MicroMDM_FailureLeavesRows(t *testing.T) {
	setupMicroMDMClear(t, http.StatusUnauthorized)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	err := clearCommandQueue(types.Device{UDID: "test-udid"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
