package director

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handlerTransport exercises the real HTTP clients without binding a local TCP port.
type handlerTransport struct{ handler http.Handler }

func (h handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	resp := w.Result()
	resp.Request = r
	return resp, nil
}

// useHTTPHandler routes the real HTTP clients at handler for the duration of the test.
// It swaps the process-global http.DefaultTransport (both the nanomdm and KMFDDM clients
// leave Transport nil, so they consult it per call), so tests using it must not call
// t.Parallel.
func useHTTPHandler(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = handlerTransport{handler}
	t.Cleanup(func() { http.DefaultTransport = previous })
}

// setupInfoRequestIntervalFlag registers/sets the info-request-interval and
// info-request-claim-ttl flags so utils.InfoRequestInterval() and
// utils.InfoRequestClaimTTL() return pinned values in tests. Flags are process-global,
// so every test here sets them rather than relying on another test's value.
func setupInfoRequestIntervalFlag(t *testing.T) {
	t.Helper()
	setIntFlag(t, "info-request-interval", 360)
	setIntFlag(t, "info-request-claim-ttl", 10)
}

// setIntFlag registers the named int flag with value, or sets it if another test or
// package already registered it.
func setIntFlag(t *testing.T, name string, value int) {
	t.Helper()
	if flag.Lookup(name) == nil {
		flag.Int(name, value, "set by "+t.Name())
		return
	}
	if err := flag.Set(name, strconv.Itoa(value)); err != nil {
		t.Fatalf("set %s: %v", name, err)
	}
}

func TestRequestDeviceUpdate_ConcurrentIdleOnlyEnqueuesOnce(t *testing.T) {
	setupNanoMDMFlag(t)
	setupInfoRequestIntervalFlag(t)
	mock, cleanup := setupMockDB(t)
	defer cleanup()
	mock.MatchExpectationsInOrder(false)

	var enqueues atomic.Int32
	useHTTPHandler(t, func(w http.ResponseWriter, r *http.Request) {
		n := enqueues.Add(1)
		_ = json.NewEncoder(w).Encode(mdm.APIResponse{CommandUUID: fmt.Sprintf("test-info-command-%d", n)})
	})
	mdm.InitClient("http://mdm.example.test", "test-key")

	device := types.Device{UDID: "test-info-device", InitialTasksRun: true}
	// Exactly one of the two concurrent claims wins: the first conditional UPDATE
	// affects its row, the second matches none because the first already moved the
	// timestamp out of the interval.
	for _, rowsAffected := range []int64{1, 0} {
		mock.ExpectQuery(`SELECT .* FROM "devices" WHERE \(lock =`).
			WillReturnRows(sqlmock.NewRows([]string{"ud_id"}))
		mock.ExpectExec(infoRequestClaimSQL).
			WithArgs(sqlmock.AnyArg(), device.UDID, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, rowsAffected))
	}
	for _, request := range []string{"ProfileList", "SecurityInfo", "DeviceInformation", "CertificateList"} {
		mockCommandInQueue(mock, device.UDID, request, "", false)
		mockGetDevice(mock, device.UDID)
		mockCreateCommand(mock)
	}
	// RequestAllDeviceInfo keeps its successful-completion stamp for other callers.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "devices" SET .*"last_info_requested"=`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			RequestDeviceUpdate(device)
		}()
	}
	close(start)
	wg.Wait()

	assert.Equal(t, int32(4), enqueues.Load(), "two concurrent Idle requests must share one four-command batch")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Require the NULL case and the conditional predicate in the atomic UPDATE itself.
const infoRequestClaimSQL = `UPDATE devices SET last_info_requested = \$1 WHERE ud_id = \$2 AND \( last_info_requested IS NULL OR last_info_requested < \$3 \)`

func TestRequestDeviceUpdate_DeniedOrFailedClaimDoesNotEnqueue(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"already claimed", nil},
		{"database error", errors.New("database unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupNanoMDMFlag(t)
			setupInfoRequestIntervalFlag(t)
			mock, cleanup := setupMockDB(t)
			defer cleanup()
			mock.ExpectQuery(`SELECT .* FROM "devices" WHERE \(lock =`).
				WillReturnRows(sqlmock.NewRows([]string{"ud_id"}))
			claim := mock.ExpectExec(infoRequestClaimSQL).
				WithArgs(sqlmock.AnyArg(), "test-info-device", sqlmock.AnyArg())
			if tc.err != nil {
				claim.WillReturnError(tc.err)
			} else {
				claim.WillReturnResult(sqlmock.NewResult(0, 0))
			}
			useHTTPHandler(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("a denied or failed claim must not send commands")
			})
			RequestDeviceUpdate(types.Device{UDID: "test-info-device"})
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// claimStampMinutesAgo matches a time argument that sits the expected number of minutes
// in the past. The claim is backdated by interval minus TTL, which is what makes it
// expire after the TTL instead of after the whole interval.
type claimStampMinutesAgo struct {
	minutes   int
	tolerance time.Duration
}

func (c claimStampMinutesAgo) Match(v driver.Value) bool {
	ts, ok := v.(time.Time)
	if !ok {
		return false
	}
	diff := ts.Sub(time.Now().Add(-time.Duration(c.minutes) * time.Minute))
	if diff < 0 {
		diff = -diff
	}
	return diff <= c.tolerance
}

// A won claim must be stamped interval-TTL in the past, so that a batch which never
// completes frees the device after the TTL. Only RequestAllDeviceInfo writes the
// current time, which extends the hold to the full interval.
func TestRequestDeviceUpdate_ClaimIsBackdatedToExpireAfterTTL(t *testing.T) {
	setupNanoMDMFlag(t)
	setupInfoRequestIntervalFlag(t)
	mock, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: "test-info-device"}
	useHTTPHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	mdm.InitClient("http://mdm.example.test", "test-key")

	mock.ExpectQuery(`SELECT .* FROM "devices" WHERE \(lock =`).
		WillReturnRows(sqlmock.NewRows([]string{"ud_id"}))
	// interval 360 minus TTL 10.
	mock.ExpectExec(infoRequestClaimSQL).
		WithArgs(claimStampMinutesAgo{minutes: 350, tolerance: time.Minute}, device.UDID, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockCommandInQueue(mock, device.UDID, "ProfileList", "", false)
	mockGetDevice(mock, device.UDID)

	RequestDeviceUpdate(device)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestDeviceUpdate_EnqueueFailureHoldsClaimUntilTTL(t *testing.T) {
	setupNanoMDMFlag(t)
	setupInfoRequestIntervalFlag(t)
	mock, cleanup := setupMockDB(t)
	defer cleanup()
	device := types.Device{UDID: "test-info-device"}
	var enqueues atomic.Int32
	useHTTPHandler(t, func(w http.ResponseWriter, r *http.Request) {
		enqueues.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	})
	mdm.InitClient("http://mdm.example.test", "test-key")

	mock.ExpectQuery(`SELECT .* FROM "devices" WHERE \(lock =`).
		WillReturnRows(sqlmock.NewRows([]string{"ud_id"}))
	mock.ExpectExec(infoRequestClaimSQL).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockCommandInQueue(mock, device.UDID, "ProfileList", "", false)
	mockGetDevice(mock, device.UDID)
	RequestDeviceUpdate(device)

	// No release on failure. A subsequent Idle check-in inside the TTL still loses the
	// claim. The device is retried once the backdated stamp falls outside the interval,
	// which happens TTL minutes after the claim rather than a whole interval later.
	mock.ExpectQuery(`SELECT .* FROM "devices" WHERE \(lock =`).
		WillReturnRows(sqlmock.NewRows([]string{"ud_id"}))
	mock.ExpectExec(infoRequestClaimSQL).
		WillReturnResult(sqlmock.NewResult(0, 0))
	RequestDeviceUpdate(device)
	assert.Equal(t, int32(1), enqueues.Load())
	require.NoError(t, mock.ExpectationsWereMet())
}
