package director

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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
	udid          string
}

func (s *infoBatchServer) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	if strings.Contains(r.URL.Path, "/v1/push/") {
		s.pushes++
		_ = json.NewEncoder(w).Encode(mdm.APIResponse{
			Status: map[string]mdm.EnrollmentStatus{s.udid: {}},
		})
		return
	}

	// The request type travels in the plist body.
	body := make([]byte, r.ContentLength)
	_, _ = r.Body.Read(body)
	requestType := ""
	for _, rt := range infoBatchRequestTypes {
		if strings.Contains(string(body), rt) {
			requestType = rt
			break
		}
	}

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

// SendCommand names the device from the payload, so an inventory batch reads the device
// row no times instead of once per command. The mock has no device-read expectation: a
// read would fail these tests.
func TestRequestAllDeviceInfo_DoesNotReReadTheDeviceRow(t *testing.T) {
	_, mockSpy, cleanup := setupInfoBatch(t, "")
	defer cleanup()

	device := types.Device{UDID: "test-udid-123", SerialNumber: "C02TEST123"}

	for _, requestType := range infoBatchRequestTypes {
		mockCommandInQueue(mockSpy, device.UDID, requestType, "", false)
		mockCreateCommand(mockSpy)
	}
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET "next_push"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET "last_info_requested"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()

	require.NoError(t, RequestAllDeviceInfo(device))
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
