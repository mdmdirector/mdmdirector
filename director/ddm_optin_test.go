package director

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupDDMFlags registers the DDM globals and resets them after the test, so a test that
// flips one can't leak that state into the next
func setupDDMFlags(t *testing.T, useDDM, useDDMPackages bool) {
	t.Helper()
	for _, name := range []string{"use-ddm", "use-ddm-packages"} {
		if flag.Lookup(name) == nil {
			flag.Bool(name, false, name)
		}
	}
	// Read by the push paths the handlers reconcile through
	if flag.Lookup("prometheus") == nil {
		flag.Bool("prometheus", false, "Enable prometheus metrics")
	}

	require.NoError(t, flag.Set("use-ddm", boolFlag(useDDM)))
	require.NoError(t, flag.Set("use-ddm-packages", boolFlag(useDDMPackages)))

	t.Cleanup(func() {
		_ = flag.Set("use-ddm", "false")
		_ = flag.Set("use-ddm-packages", "false")
	})
}

func boolFlag(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// optInRows builds a ddm_opt_ins result set for the given UDIDs. The column is
// device_ud_id: gorm derives it from the DeviceUDID field, same as everywhere else
// in the schema
func optInRows(udids ...string) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"device_ud_id"})
	for _, udid := range udids {
		rows.AddRow(udid)
	}
	return rows
}

func testDevices(udids ...string) []types.Device {
	devices := make([]types.Device, 0, len(udids))
	for _, udid := range udids {
		devices = append(devices, types.Device{UDID: udid, SerialNumber: "SERIAL-" + udid})
	}
	return devices
}

// --- ddmForDevice / ddmPackagesForDevice ---

// The global flag alone decides the answer, so no opt-in lookup should be issued at all.
// The mock has zero expectations: any query would error, and the count would come back 0
func TestDDMForDevice_GlobalOn_SkipsOptInQuery(t *testing.T) {
	setupDDMFlags(t, true, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	assert.True(t, ddmForDevice(types.Device{UDID: "udid-1"}))
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestDDMForDevice_OptIn(t *testing.T) {
	tests := []struct {
		name     string
		optInRow bool
		expected bool
	}{
		{name: "opted in", optInRow: true, expected: true},
		{name: "not opted in", optInRow: false, expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupDDMFlags(t, false, false)
			mockSpy, cleanup := setupMockDB(t)
			defer cleanup()

			count := 0
			if tc.optInRow {
				count = 1
			}
			mockSpy.ExpectQuery(`SELECT count\(\*\) FROM "ddm_opt_ins" WHERE device_ud_id = \$1`).
				WithArgs("udid-1").
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))

			assert.Equal(t, tc.expected, ddmForDevice(types.Device{UDID: "udid-1"}))
			assert.NoError(t, mockSpy.ExpectationsWereMet())
		})
	}
}

// An empty UDID can't be opted in and must not reach the database
func TestDDMForDevice_EmptyUDID(t *testing.T) {
	setupDDMFlags(t, false, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	assert.False(t, ddmForDevice(types.Device{}))
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestDDMPackagesForDevice_GlobalOn_SkipsOptInQuery(t *testing.T) {
	setupDDMFlags(t, false, true)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	assert.True(t, ddmPackagesForDevice(types.Device{UDID: "udid-1"}))
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// The two globals are independent: USE_DDM covers profiles only, so an application
// decision still falls through to the opt-in list
func TestDDMPackagesForDevice_ProfileGlobalDoesNotEnablePackages(t *testing.T) {
	setupDDMFlags(t, true, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(`SELECT count\(\*\) FROM "ddm_opt_ins" WHERE device_ud_id = \$1`).
		WithArgs("udid-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	assert.False(t, ddmPackagesForDevice(types.Device{UDID: "udid-1"}))
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestDDMPackagesForDevice_OptIn(t *testing.T) {
	setupDDMFlags(t, false, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(`SELECT count\(\*\) FROM "ddm_opt_ins" WHERE device_ud_id = \$1`).
		WithArgs("udid-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	assert.True(t, ddmPackagesForDevice(types.Device{UDID: "udid-1"}))
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// --- partitioning ---

// The cohort split comes from one batched query, and order within each cohort is preserved
func TestPartitionByDDM_CohortSplit(t *testing.T) {
	setupDDMFlags(t, false, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(`SELECT \* FROM "ddm_opt_ins" WHERE device_ud_id IN \(\$1,\$2,\$3\)`).
		WithArgs("udid-1", "udid-2", "udid-3").
		WillReturnRows(optInRows("udid-2"))

	ddmDevices, legacyDevices := partitionByDDM(testDevices("udid-1", "udid-2", "udid-3"))

	require.Len(t, ddmDevices, 1)
	assert.Equal(t, "udid-2", ddmDevices[0].UDID)
	require.Len(t, legacyDevices, 2)
	assert.Equal(t, "udid-1", legacyDevices[0].UDID)
	assert.Equal(t, "udid-3", legacyDevices[1].UDID)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// No opt-in rows means every device stays on the classic protocol
func TestPartitionByDDM_NoOptIns(t *testing.T) {
	setupDDMFlags(t, false, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(`SELECT \* FROM "ddm_opt_ins"`).
		WillReturnRows(optInRows())

	ddmDevices, legacyDevices := partitionByDDM(testDevices("udid-1", "udid-2"))

	assert.Empty(t, ddmDevices)
	assert.Len(t, legacyDevices, 2)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// With the global on every device is on DDM, so the opt-in query is skipped entirely
func TestPartitionByDDM_GlobalOn_SkipsOptInQuery(t *testing.T) {
	setupDDMFlags(t, true, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	ddmDevices, legacyDevices := partitionByDDM(testDevices("udid-1", "udid-2"))

	assert.Len(t, ddmDevices, 2)
	assert.Empty(t, legacyDevices)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// partitionByDDMPackages reads USE_DDM_PACKAGES, not USE_DDM
func TestPartitionByDDMPackages_ReadsPackagesGlobal(t *testing.T) {
	setupDDMFlags(t, false, true)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	ddmDevices, legacyDevices := partitionByDDMPackages(testDevices("udid-1"))

	assert.Len(t, ddmDevices, 1)
	assert.Empty(t, legacyDevices)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestPartitionByDDMPackages_CohortSplit(t *testing.T) {
	setupDDMFlags(t, false, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(`SELECT \* FROM "ddm_opt_ins" WHERE device_ud_id IN \(\$1,\$2\)`).
		WithArgs("udid-1", "udid-2").
		WillReturnRows(optInRows("udid-1"))

	ddmDevices, legacyDevices := partitionByDDMPackages(testDevices("udid-1", "udid-2"))

	require.Len(t, ddmDevices, 1)
	assert.Equal(t, "udid-1", ddmDevices[0].UDID)
	require.Len(t, legacyDevices, 1)
	assert.Equal(t, "udid-2", legacyDevices[0].UDID)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// An empty device list short-circuits: nothing to look up
func TestPartitionByDDM_NoDevices(t *testing.T) {
	setupDDMFlags(t, false, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	ddmDevices, legacyDevices := partitionByDDM(nil)

	assert.Empty(t, ddmDevices)
	assert.Empty(t, legacyDevices)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// --- handlers ---

// serveDeviceDDM routes a request through a mux router so {udid} is populated
func serveDeviceDDM(t *testing.T, method, udid string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/device/"+udid+"/ddm", nil)

	router := mux.NewRouter()
	router.HandleFunc("/device/{udid}/ddm", EnableDeviceDDMHandler).Methods(http.MethodPost)
	router.HandleFunc("/device/{udid}/ddm", DisableDeviceDDMHandler).Methods(http.MethodDelete)
	router.ServeHTTP(rr, req)

	return rr
}

// expectDeviceNotFound mocks the GetDevice lookup returning no rows
func expectDeviceNotFound(mockSpy sqlmock.Sqlmock, udid string) {
	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1`).
		WithArgs(udid).
		WillReturnRows(sqlmock.NewRows([]string{"ud_id", "serial_number"}))
}

// decodeDDMStatus reads the handler's JSON body
func decodeDDMStatus(t *testing.T, rr *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	return body
}

func TestEnableDeviceDDMHandler_UnknownDevice(t *testing.T) {
	setupDDMFlags(t, false, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	expectDeviceNotFound(mockSpy, "missing-udid")

	rr := serveDeviceDDM(t, http.MethodPost, "missing-udid")

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestDisableDeviceDDMHandler_UnknownDevice(t *testing.T) {
	setupDDMFlags(t, false, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	expectDeviceNotFound(mockSpy, "missing-udid")

	rr := serveDeviceDDM(t, http.MethodDelete, "missing-udid")

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// Enabling inserts the opt-in row. The reconcile that follows can't succeed against a
// mock DB, which is the interesting part: the row still stands and the handler reports
// the partial failure rather than a 5xx
func TestEnableDeviceDDMHandler_CreatesOptInRow(t *testing.T) {
	setupDDMFlags(t, false, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockGetDevice(mockSpy, "udid-1")

	// FirstOrCreate: look for an existing row, then insert
	mockSpy.ExpectQuery(`SELECT \* FROM "ddm_opt_ins"`).
		WithArgs("udid-1", "udid-1").
		WillReturnRows(optInRows())
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`INSERT INTO "ddm_opt_ins"`).
		WithArgs("udid-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()

	rr := serveDeviceDDM(t, http.MethodPost, "udid-1")

	body := decodeDDMStatus(t, rr)
	assert.Equal(t, true, body["use_ddm"])
	assert.Equal(t, http.StatusMultiStatus, rr.Code)
	assert.Equal(t, false, body["reconciled"])
	assert.NotEmpty(t, body["reconcile_errors"])
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// Disabling tears down declarations, then deletes the opt-in row. With no profiles to
// remove the teardown makes no KMFDDM calls, so it reports no errors
func TestDisableDeviceDDMHandler_DeletesOptInRow(t *testing.T) {
	setupDDMFlags(t, false, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockGetDevice(mockSpy, "udid-1")

	// teardownDDMForDevice: no device profiles and no shared profiles to tear down
	mockSpy.ExpectQuery(`SELECT \* FROM "device_profiles"`).
		WillReturnRows(sqlmock.NewRows([]string{"device_ud_id"}))
	mockSpy.ExpectQuery(`SELECT \* FROM "shared_profiles"`).
		WillReturnRows(sqlmock.NewRows([]string{"payload_identifier"}))

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`DELETE FROM "ddm_opt_ins" WHERE device_ud_id = \$1`).
		WithArgs("udid-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	rr := serveDeviceDDM(t, http.MethodDelete, "udid-1")

	body := decodeDDMStatus(t, rr)
	assert.Equal(t, false, body["use_ddm"])
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// A failed opt-in write is a real error: the caller must not be told the device is on DDM
func TestEnableDeviceDDMHandler_OptInWriteFails(t *testing.T) {
	setupDDMFlags(t, false, false)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockGetDevice(mockSpy, "udid-1")

	mockSpy.ExpectQuery(`SELECT \* FROM "ddm_opt_ins"`).
		WillReturnRows(optInRows())
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`INSERT INTO "ddm_opt_ins"`).
		WillReturnError(assert.AnError)
	mockSpy.ExpectRollback()

	rr := serveDeviceDDM(t, http.MethodPost, "udid-1")

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.NotContains(t, rr.Body.String(), "use_ddm")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
