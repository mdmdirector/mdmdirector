package director

import (
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

// TestRunCleanup verifies the maintenance mutations are issued in order: delete orphaned
// certificates and profile lists, expire stale random unlock PINs, and reset fixed PINs.
func TestRunCleanup(t *testing.T) {
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

	updateNextPush("test-udid")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
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
