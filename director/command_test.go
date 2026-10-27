package director

import (
	"bytes"
	"database/sql/driver"
	"flag"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestExampleHowToUseSqlmock(t *testing.T) {
	dbMock, mock, err := sqlmock.New()
	if err != nil {
		t.Errorf("Fail to get SQL mock")
	}
	defer dbMock.Close()

	postgresMock, _, err := sqlmock.New()
	if err != nil {
		t.Errorf("Fail to get postgres mock")
	}

	_, err = gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	assert.Equal(t, nil, err)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %s", err)
	}
}

func TestClearCommands(t *testing.T) {
	// Old way of overriding flags... this doesn't work because flag.Parse() cannot be called multiple times
	// in the same process.
	// var tmp bool
	// os.Args = []string{"-clear-device-on-enroll", "true"}
	// flag.BoolVar(&tmp, "clear-device-on-enroll", true, "Deletes device profiles and install applications when a device enrolls")
	// flag.Parse()

	// New way of overriding flags:
	utils.FlagProvider = mockFlagBuilder{false}

	postgresMock, mockSpy, err := sqlmock.New()
	if err != nil {
		t.Errorf("Fail to get postgres mock")
	}
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^DELETE FROM "commands" WHERE device_ud_id = \$1 AND NOT status IN \(\$2,\$3,\$4\)`).WithArgs(
		"1234-5678-123456",
		"Error",
		"Acknowledged",
		commandStatusRetriedViaDDM,
	).WillReturnResult(sqlmock.NewResult(0, 0))
	mockSpy.ExpectCommit()

	device := types.Device{
		SerialNumber: "C02ABCDEFGH",
		UDID:         "1234-5678-123456",
	}
	err = ClearCommands(&device)

	assert.Equal(t, nil, err)
}

func TestClearCommands_ClearDeviceOnEnroll(t *testing.T) {
	utils.FlagProvider = mockFlagBuilder{true}

	// Set up Database Mocks
	postgresMock, mockSpy, _ := sqlmock.New()
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	// Set up Database expectations
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^DELETE FROM "commands" WHERE device_ud_id = \$1 AND NOT status IN \(\$2,\$3,\$4\)`).WithArgs(
		"1234-5678-123456",
		"Error",
		"Acknowledged",
		commandStatusRetriedViaDDM,
	).WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^DELETE FROM "device_profiles" WHERE device_ud_id = \$1`).WithArgs(
		"1234-5678-123456",
	).WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^DELETE FROM "device_install_applications" WHERE device_ud_id = \$1`).WithArgs(
		"1234-5678-123456",
	).WillReturnResult(sqlmock.NewResult(0, 0))
	mockSpy.ExpectCommit()

	device := types.Device{
		SerialNumber: "C02ABCDEFGH",
		UDID:         "1234-5678-123456",
	}
	err := ClearCommands(&device)

	assert.Equal(t, nil, err)
}

func TestClearCommands_OnDeleteError(t *testing.T) {
	postgresMock, mockSpy, _ := sqlmock.New()
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{SkipDefaultTransaction: true})
	db.DB = DB

	mockSpy.ExpectExec(`.*`).WithArgs(
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
	).WillReturnError(errors.New("database has gone away"))

	device := types.Device{
		SerialNumber: "C02ABCDEFGH",
		UDID:         "1234-5678-123456",
	}
	err := ClearCommands(&device)

	assert.NotEmpty(t, err)
	assert.Equal(t, "Failed to clear Command Queue for 1234-5678-123456: database has gone away", err.Error())
}

// // Test classes
type mockFlagBuilder struct {
	doClear bool
}

func (m mockFlagBuilder) ClearDeviceOnEnroll() bool {
	return m.doClear
}

func TestInspectCommandQueue(t *testing.T) {
	// Ensure we use the microMDM code path (flag may be set to nanomdm by other tests)
	if flag.Lookup("mdm-server-type") != nil {
		_ = flag.Set("mdm-server-type", "micromdm")
	}

	// Mock the HTTP client and response
	var path string
	body := []byte(`test`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, err := w.Write(body)
		if err != nil {
			t.Error(err.Error())
		}
	}))
	defer server.Close()
	// These need to be set due to global variable referencing. Guard registration since
	// other tests in this package may have already registered them.
	if flag.Lookup("micromdmurl") == nil {
		flag.String("micromdmurl", server.URL, "MicroMDM Server URL")
	} else {
		_ = flag.Set("micromdmurl", server.URL)
	}
	if flag.Lookup("micromdmapikey") == nil {
		flag.String("micromdmapikey", "", "MicroMDM Server API Key")
	}
	device := types.Device{
		UDID: "1234-5678-123456",
	}

	// Call the function to inspect the command queue
	haveBody, err := InspectCommandQueue(device)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !bytes.Equal(haveBody, body) {
		t.Error("Expected body to be equal")
	}
	if path != "/v1/commands/1234-5678-123456" {
		t.Errorf("Expected path to be /v1/commands/1234-5678-123456, got %s", path)
	}
}

// staleDelete matches one batch of expireStaleCommands: a DELETE whose rows come from a
// bounded subquery, with the statuses written into the SQL.
const staleDelete = `^DELETE FROM "commands" WHERE command_uuid IN \(SELECT "command_uuid" FROM "commands" WHERE status = '' AND updated_at < \$1 AND request_type NOT IN \(\$2,\$3\) LIMIT 10000\)$`

func setupStaleMock(t *testing.T) sqlmock.Sqlmock {
	t.Helper()
	postgresMock, mockSpy, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Fail to get postgres mock: %v", err)
	}
	t.Cleanup(func() { postgresMock.Close() })
	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB
	return mockSpy
}

func expectStaleBatch(mockSpy sqlmock.Sqlmock, cutoff driver.Value, deleted int64) {
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(staleDelete).
		WithArgs(cutoff, "DeviceLock", "EraseDevice").
		WillReturnResult(sqlmock.NewResult(0, deleted))
	mockSpy.ExpectCommit()
}

func TestExpireStaleCommands_DeletesOldUnresolved(t *testing.T) {
	setupStaleCommandThresholdFlag(t)
	mockSpy := setupStaleMock(t)

	// Fewer than a batch: one DELETE and done.
	expectStaleBatch(mockSpy, sqlmock.AnyArg(), 3)

	err := expireStaleCommands()

	assert.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestExpireStaleCommands_DeletesInBatchesUntilShort(t *testing.T) {
	setupStaleCommandThresholdFlag(t)
	mockSpy := setupStaleMock(t)

	// A backlog larger than one batch, and far larger than PostgreSQL's 65535 bind
	// parameters: full batches repeat, the first short one ends the loop.
	for i := 0; i < 7; i++ {
		expectStaleBatch(mockSpy, sqlmock.AnyArg(), staleCommandDeleteBatch)
	}
	expectStaleBatch(mockSpy, sqlmock.AnyArg(), 42)

	err := expireStaleCommands()

	assert.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestExpireStaleCommands_DeleteError(t *testing.T) {
	setupStaleCommandThresholdFlag(t)
	mockSpy := setupStaleMock(t)

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(staleDelete).WillReturnError(errors.New("statement timeout"))
	mockSpy.ExpectRollback()

	err := expireStaleCommands()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expireStaleCommands: delete")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// cutoffNear matches a time.Time argument that falls within +/-2s of want, so the
// test can assert on the threshold actually used without racing the clock.
type cutoffNear struct{ want time.Time }

func (c cutoffNear) Match(v driver.Value) bool {
	got, ok := v.(time.Time)
	if !ok {
		return false
	}
	delta := got.Sub(c.want)
	return delta > -2*time.Second && delta < 2*time.Second
}

func TestExpireStaleCommands_RespectsConfiguredThreshold(t *testing.T) {
	setupStaleCommandThresholdFlag(t)
	defer func() { _ = flag.Set("stale-command-threshold", "30") }()
	_ = flag.Set("stale-command-threshold", "5")
	mockSpy := setupStaleMock(t)

	expectStaleBatch(mockSpy, cutoffNear{time.Now().Add(-5 * time.Minute)}, 0)

	err := expireStaleCommands()

	assert.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestExpireStaleCommands_NoneStale(t *testing.T) {
	setupStaleCommandThresholdFlag(t)
	mockSpy := setupStaleMock(t)

	expectStaleBatch(mockSpy, sqlmock.AnyArg(), 0)

	err := expireStaleCommands()

	assert.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
