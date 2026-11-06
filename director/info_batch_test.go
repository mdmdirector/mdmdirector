package director

import (
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/groob/plist"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// infoBatchServer stands in for NanoMDM and records how its enqueue and push endpoints
// were called. failEnqueueFor names a request type whose enqueue answers with a
// per-device command error.
type infoBatchServer struct {
	mu            sync.Mutex
	enqueues      []string
	noPushFlags   []bool
	pushes        int
	failEnqueueOn string
	failPush      bool
	udid          string
}

func (s *infoBatchServer) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	if strings.Contains(r.URL.Path, "/v1/push/") {
		s.pushes++
		status := mdm.EnrollmentStatus{}
		if s.failPush {
			status.PushError = "APNs unavailable"
		}
		_ = json.NewEncoder(w).Encode(mdm.APIResponse{
			Status: map[string]mdm.EnrollmentStatus{s.udid: status},
		})
		return
	}

	requestType := enqueuedRequestType(r)

	s.enqueues = append(s.enqueues, requestType)
	s.noPushFlags = append(s.noPushFlags, r.URL.Query().Get("nopush") == "1")

	if requestType == s.failEnqueueOn {
		_ = json.NewEncoder(w).Encode(mdm.APIResponse{
			Status: map[string]mdm.EnrollmentStatus{s.udid: {CommandError: "enqueue refused"}},
		})
		return
	}

	_ = json.NewEncoder(w).Encode(mdm.APIResponse{
		CommandUUID: "uuid-" + requestType,
		RequestType: requestType,
		Status:      map[string]mdm.EnrollmentStatus{s.udid: {}},
	})
}

// enqueuedRequestType decodes the RequestType out of the plist an enqueue carries,
// rather than matching substrings against the raw body.
func enqueuedRequestType(r *http.Request) string {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return ""
	}

	var command struct {
		Command struct {
			RequestType string
		}
	}
	if err := plist.Unmarshal(body, &command); err != nil {
		return ""
	}

	return command.Command.RequestType
}

func setupInfoBatch(t *testing.T, failEnqueueOn string) (*infoBatchServer, sqlmock.Sqlmock, func()) {
	t.Helper()
	setupNanoMDMFlag(t)
	if flag.Lookup("once-in") == nil {
		flag.Int("once-in", 60, "Minutes between scheduled pushes")
	}

	spy := &infoBatchServer{failEnqueueOn: failEnqueueOn, udid: "test-udid-123"}
	server := httptest.NewServer(http.HandlerFunc(spy.handler))
	t.Cleanup(server.Close)
	mdm.InitClient(server.URL, "test-api-key")

	mockSpy, cleanup := setupMockDB(t)
	mockSpy.MatchExpectationsInOrder(false)

	return spy, mockSpy, cleanup
}

// A clean batch enqueues all four commands with the push suppressed, then sends exactly
// one push. The old code pushed on every enqueue, so one batch woke the device four
// times and each wake returned as another Idle check-in.
func TestRequestAllDeviceInfo_EnqueuesFourCommandsAndPushesOnce(t *testing.T) {
	spy, mockSpy, cleanup := setupInfoBatch(t, "")
	defer cleanup()

	device := types.Device{UDID: "test-udid-123", SerialNumber: "C02TEST123"}

	for _, requestType := range infoBatchRequestTypes {
		mockCommandInQueue(mockSpy, device.UDID, requestType, "", false)
		mockCreateCommand(mockSpy)
	}
	// The push records the device's next scheduled-push time.
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET "next_push"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()
	// A batch that completes stamps last_info_requested, which extends the claim to the
	// full interval.
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET "last_info_requested"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()

	err := RequestAllDeviceInfo(device)
	require.NoError(t, err)

	assert.Equal(t, infoBatchRequestTypes, spy.enqueues, "all four inventory commands are enqueued")
	assert.Equal(t, 1, spy.pushes, "one push per batch, not one per command")
	for i, noPush := range spy.noPushFlags {
		assert.True(t, noPush, "enqueue %d suppresses its own push", i)
	}
}

// A batch whose last enqueue fails still pushes what was queued, so the commands that
// did land are delivered instead of waiting for something else to push. The failure is
// still returned, which leaves last_info_requested unstamped so the claim lapses after
// its TTL.
func TestRequestAllDeviceInfo_PushesPartialBatchAndReportsFailure(t *testing.T) {
	spy, mockSpy, cleanup := setupInfoBatch(t, "CertificateList")
	defer cleanup()

	device := types.Device{UDID: "test-udid-123", SerialNumber: "C02TEST123"}

	for _, requestType := range infoBatchRequestTypes {
		mockCommandInQueue(mockSpy, device.UDID, requestType, "", false)
		if requestType != "CertificateList" {
			mockCreateCommand(mockSpy)
		}
	}
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET "next_push"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()

	err := RequestAllDeviceInfo(device)

	require.Error(t, err, "the failed enqueue is reported")
	assert.Contains(t, err.Error(), "CertificateList")
	assert.Equal(t, 1, spy.pushes, "the three queued commands are still pushed")
	assert.NoError(t, mockSpy.ExpectationsWereMet(), "last_info_requested is not stamped")
}

// A batch whose commands are all still queued sends one push and no enqueues. This is
// the retry after a push failed: the queue gets moved without being resent.
func TestRequestAllDeviceInfo_PushesWithoutReEnqueuingAQueuedBatch(t *testing.T) {
	spy, mockSpy, cleanup := setupInfoBatch(t, "")
	defer cleanup()

	device := types.Device{UDID: "test-udid-123", SerialNumber: "C02TEST123"}

	for _, requestType := range infoBatchRequestTypes {
		mockCommandInQueue(mockSpy, device.UDID, requestType, "", true)
	}
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET "next_push"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET "last_info_requested"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()

	require.NoError(t, RequestAllDeviceInfo(device))

	assert.Empty(t, spy.enqueues, "nothing is enqueued a second time")
	assert.Equal(t, 1, spy.pushes, "the queued batch is still pushed")
}

// A failed push is logged, not returned. RunInitialTasks treats an error here as fatal
// and returns before InstallAllProfiles and DeviceConfigured, so returning a transient
// push failure would hold a device in Setup Assistant. The commands stay queued, a
// failed push leaves next_push alone, and the scheduled push loop collects the device.
func TestRequestAllDeviceInfo_FailedPushIsNotFatal(t *testing.T) {
	spy, mockSpy, cleanup := setupInfoBatch(t, "")
	defer cleanup()
	spy.failPush = true

	device := types.Device{UDID: "test-udid-123", SerialNumber: "C02TEST123"}

	for _, requestType := range infoBatchRequestTypes {
		mockCommandInQueue(mockSpy, device.UDID, requestType, "", false)
		mockCreateCommand(mockSpy)
	}
	// No next_push update: PushDevice only records one when the push succeeded.
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET "last_info_requested"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()

	require.NoError(t, RequestAllDeviceInfo(device), "a failed push does not fail the batch")

	assert.Equal(t, infoBatchRequestTypes, spy.enqueues, "all four commands are still queued")
	assert.Equal(t, 1, spy.pushes)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
