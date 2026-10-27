package director

import (
	"database/sql/driver"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupOnceInFlag registers/sets the once-in flag so utils.OnceIn() works in tests.
func setupOnceInFlag(t *testing.T) {
	t.Helper()
	if flag.Lookup("once-in") == nil {
		flag.Int("once-in", 180, "once in")
	} else {
		_ = flag.Set("once-in", "180")
	}
}

// setupStaleCommandThresholdFlag registers/sets the stale-command-threshold flag so
// utils.StaleCommandThreshold() works in tests.
func setupStaleCommandThresholdFlag(t *testing.T) {
	t.Helper()
	if flag.Lookup("stale-command-threshold") == nil {
		flag.Int("stale-command-threshold", 30, "stale command threshold")
	} else {
		_ = flag.Set("stale-command-threshold", "30")
	}
}

// TestRunCleanup verifies the maintenance mutations are issued in order: delete orphaned
// certificates and profile lists, expire stale random unlock PINs, and reset fixed PINs.
func TestRunCleanup(t *testing.T) {
	setupStaleCommandThresholdFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`DELETE FROM "certificates"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mockSpy.ExpectCommit()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`DELETE FROM "profile_lists"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mockSpy.ExpectCommit()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`DELETE FROM "unlock_pins"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mockSpy.ExpectCommit()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET "unlock_pin"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mockSpy.ExpectCommit()

	expectStaleBatch(mockSpy, sqlmock.AnyArg(), 0)

	err := runCleanup()
	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestUpdateNextPush verifies a device's next_push is persisted.
func TestUpdateNextPush(t *testing.T) {
	setupOnceInFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET "next_push"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	updateNextPush("test-udid", time.Hour)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestPushDeviceNoEnrollmentBacksOff verifies a push that fails because NanoMDM has no
// enrollment for the device sets next_push about noEnrollmentPushBackoff ahead, so the
// next scans skip it instead of failing again every time.
func TestPushDeviceNoEnrollmentBacksOff(t *testing.T) {
	setupNanoMDMFlag(t)
	setupOnceInFlag(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := mdm.APIResponse{
			Status: map[string]mdm.EnrollmentStatus{
				"udid-orphan": {PushError: mdm.PushErrorNoEnrollment},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)
	mdm.InitClient(server.URL, "test-api-key")

	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	minNext := time.Now().Add(noEnrollmentPushBackoff - time.Minute)
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET "next_push"`).
		WithArgs(timeAfter{minNext}, sqlmock.AnyArg(), "udid-orphan"). // next_push, updated_at, ud_id
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	err := PushDevice("udid-orphan")
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoNanoMDMEnrollment)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestPushDeviceOtherErrorNoBackoff verifies other push errors leave next_push alone,
// so the device is retried on the next scan as before.
func TestPushDeviceOtherErrorNoBackoff(t *testing.T) {
	setupNanoMDMFlag(t)
	setupOnceInFlag(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := mdm.APIResponse{
			Status: map[string]mdm.EnrollmentStatus{
				"udid-abc": {PushError: "BadDeviceToken"},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)
	mdm.InitClient(server.URL, "test-api-key")

	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	err := PushDevice("udid-abc")
	require.Error(t, err)
	assert.NotErrorIs(t, err, errNoNanoMDMEnrollment)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// timeAfter matches a time.Time SQL argument that is after t.
type timeAfter struct{ t time.Time }

func (a timeAfter) Match(v driver.Value) bool {
	got, ok := v.(time.Time)
	return ok && got.After(a.t)
}

// TestPushDeviceUpdatesNextPush verifies a successful NanoMDM push persists next_push, so
// the next control-plane scan skips the device until it is due again.
func TestPushDeviceUpdatesNextPush(t *testing.T) {
	setupNanoMDMFlag(t)
	setupOnceInFlag(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := mdm.APIResponse{
			Status: map[string]mdm.EnrollmentStatus{
				"udid-abc": {PushResult: "success"},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)
	mdm.InitClient(server.URL, "test-api-key")

	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET "next_push"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	err := PushDevice("udid-abc")
	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestDeviceNeedsPush verifies the NextPush guard actually throttles: a device with a
// future NextPush is skipped, and one with a past NextPush is eligible again.
func TestDeviceNeedsPush(t *testing.T) {
	recent := types.Device{
		UDID:                "recent",
		NextPush:            time.Now().Add(1 * time.Hour),
		LastCertificateList: time.Now(),
		LastProfileList:     time.Now(),
		LastSecurityInfo:    time.Now(),
		LastDeviceInfo:      time.Now(),
	}
	assert.False(t, deviceNeedsPush(recent), "device pushed recently (future NextPush) should be skipped")

	due := recent
	due.UDID = "due"
	due.NextPush = time.Now().Add(-1 * time.Hour)
	assert.True(t, deviceNeedsPush(due), "device past its NextPush should be eligible")
}
