package director

import (
	"flag"
	"net/http"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// profileListReporting is the ProfileList a device returns when the profile is present
// with the UUID mdmdirector expects.
func profileListReporting() types.ProfileListData {
	identifier, uuidStr := retryTestProfileID, retryTestHash
	return types.ProfileListData{ProfileList: []types.ProfileList{{
		ID:                 uuid.New(),
		PayloadIdentifier:  identifier,
		PayloadUUID:        uuidStr,
		PayloadDisplayName: "Test",
	}}}
}

// mockReplaceProfileList is the association replace VerifyMDMProfiles starts with.
func mockReplaceProfileList(mockSpy sqlmock.Sqlmock) {
	// gorm saves the parent and upserts the new rows in one transaction, then unlinks
	// the rows no longer in the list in a second one.
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "devices" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectQuery(`INSERT INTO "profile_lists"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mockSpy.ExpectCommit()
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "profile_lists" SET "device_ud_id"=`).WillReturnResult(sqlmock.NewResult(0, 0))
	mockSpy.ExpectCommit()
}

// mockVerifyLoads covers the loads VerifyMDMProfiles does after the replace: the device's
// profiles (one, installed, at retryTestHash), no shared profiles, and the errored-install
// query answering that the profile's last InstallProfile at that content was Error.
func mockVerifyLoads(mockSpy sqlmock.Sqlmock, lastStatus string, lastAttempt int) {
	mockSpy.ExpectQuery(`SELECT \* FROM "device_profiles" WHERE device_ud_id = \$1`).
		WithArgs(retryTestUDID).
		WillReturnRows(sqlmock.NewRows([]string{"payload_identifier", "device_ud_id", "hashed_payload_uuid", "mobileconfig_data", "installed"}).
			AddRow(retryTestProfileID, retryTestUDID, retryTestHash, []byte("<plist/>"), true))
	mockSpy.ExpectQuery(`SELECT \* FROM "shared_profiles"`).
		WillReturnRows(sqlmock.NewRows([]string{"payload_identifier"}))
	mockSpy.ExpectQuery(`SELECT DISTINCT ON \(identifier\) identifier, content_hash, status, attempt_count FROM commands WHERE device_ud_id = \$1 AND request_type = \$2 ORDER BY identifier, updated_at DESC`).
		WithArgs(retryTestUDID, "InstallProfile").
		WillReturnRows(sqlmock.NewRows([]string{"identifier", "content_hash", "status", "attempt_count"}).AddRow(retryTestProfileID, retryTestHash, lastStatus, lastAttempt))
}

// TestVerifyMDMProfiles_ReinstallsProfileWhoseLastInstallErrored: the device reports the
// profile as present with the right UUID, which used to end the check. Because its last
// InstallProfile came back Error with the fast retries exhausted (attempt 3 of 3), one
// slow-tier InstallProfile is sent, numbered 4.
func TestVerifyMDMProfiles_ReinstallsProfileWhoseLastInstallErrored(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	setupRetryFlags(t, 2)

	mockReplaceProfileList(mockSpy)
	mockVerifyLoads(mockSpy, "Error", 3)
	mockNotOptedIntoDDM(mockSpy)
	// PushProfiles (legacy): nothing pending for this content, so enqueue at attempt 4
	mockCommandInQueueWithHash(mockSpy, retryTestUDID, "InstallProfile", retryTestProfileID, retryTestHash, false)
	mockGetDevice(mockSpy, retryTestUDID)
	mockInsertRetryCommand(mockSpy, 4)

	err := VerifyMDMProfiles(profileListReporting(), types.Device{UDID: retryTestUDID, SerialNumber: "C02TEST123"})

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestVerifyMDMProfiles_StopsAtTotalRetries: the same situation once the slow tier has
// also been used up (attempt 5 of 5). The profile is left alone until its content changes.
func TestVerifyMDMProfiles_StopsAtTotalRetries(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	setupRetryFlags(t, 3) // total = 5

	mockReplaceProfileList(mockSpy)
	mockVerifyLoads(mockSpy, "Error", 5)
	mockNotOptedIntoDDM(mockSpy)

	err := VerifyMDMProfiles(profileListReporting(), types.Device{UDID: retryTestUDID})

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestVerifyMDMProfiles_PresentAndAcknowledgedIsLeftAlone is the control: same ProfileList,
// but the last InstallProfile succeeded, so nothing is pushed.
func TestVerifyMDMProfiles_PresentAndAcknowledgedIsLeftAlone(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	setupRetryFlags(t, 2)

	mockReplaceProfileList(mockSpy)
	mockVerifyLoads(mockSpy, "Acknowledged", 0)
	mockNotOptedIntoDDM(mockSpy)

	err := VerifyMDMProfiles(profileListReporting(), types.Device{UDID: retryTestUDID})

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestVerifyMDMProfiles_DDMDeviceTouchesOnceAndMarksError: on a device whose profiles are
// managed via DDM the reinstall is a declaration touch, and the Error row is re-labelled so
// the next ProfileList does not touch again.
func TestVerifyMDMProfiles_DDMDeviceTouchesOnceAndMarksError(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()
	setupRetryFlags(t, 2)
	kmfddm, requests, statusOverrides := newMockKMFDDM(t)
	t.Cleanup(kmfddm.Close)
	ddm.InitClient(kmfddm.URL, "test-key")
	if flag.Lookup("nanomdm-profile-url") == nil {
		flag.String("nanomdm-profile-url", "", "profile url")
	}
	require.NoError(t, flag.Set("nanomdm-profile-url", "https://mdm.example.com"))
	// Declarations already exist unchanged, so the PUTs answer 304 and the push falls back to a touch.
	statusOverrides["PUT /v1/declarations"] = http.StatusNotModified

	mockReplaceProfileList(mockSpy)
	mockVerifyLoads(mockSpy, "Error", 3)
	// ddmForDevice: opted in
	mockSpy.ExpectQuery(`SELECT count\(\*\) FROM "ddm_opt_ins" WHERE device_ud_id = \$1`).
		WithArgs(retryTestUDID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	// after the DDM push, the Error row is marked
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`UPDATE "commands" SET "status"=\$1,"updated_at"=\$2 WHERE device_ud_id = \$3 AND request_type = \$4 AND status = \$5 AND identifier IN \(\$6\)`).
		WithArgs(commandStatusRetriedViaDDM, sqlmock.AnyArg(), retryTestUDID, "InstallProfile", "Error", retryTestProfileID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	err := VerifyMDMProfiles(profileListReporting(), types.Device{UDID: retryTestUDID})

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())

	var touches, notifies int
	for _, r := range *requests {
		if r.Method == "POST" && strings.Contains(r.Path, "/touch") {
			touches++
		}
		if strings.Contains(r.Path, "/notify") {
			notifies++
		}
	}
	assert.Equal(t, 2, touches, "legacy and activation declarations touched")
	assert.Equal(t, 1, notifies, "device notified once")
}
