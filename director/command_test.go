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
	mockSpy.ExpectExec(`^DELETE FROM "commands" WHERE device_ud_id = \$1 AND NOT \(status = \$2 OR status = \$3\)`).WithArgs(
		"1234-5678-123456",
		"Error",
		"Acknowledged",
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
	mockSpy.ExpectExec(`^DELETE FROM "commands" WHERE device_ud_id = \$1 AND NOT \(status = \$2 OR status = \$3\)`).WithArgs(
		"1234-5678-123456",
		"Error",
		"Acknowledged",
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

func TestExpireStaleCommands_DeletesOldUnresolved(t *testing.T) {
	setupStaleCommandThresholdFlag(t)

	postgresMock, mockSpy, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Fail to get postgres mock: %v", err)
	}
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	rows := sqlmock.NewRows([]string{"command_uuid", "status", "device_ud_id", "request_type", "updated_at"}).
		AddRow("stale-uuid-1", "", "1234-5678-123456", "InstallProfile", time.Now().Add(-time.Hour))
	mockSpy.ExpectQuery(`^SELECT \* FROM "commands" WHERE status = \$1 AND updated_at < \$2 AND request_type NOT IN \(\$3,\$4\)`).
		WithArgs("", sqlmock.AnyArg(), "DeviceLock", "EraseDevice").
		WillReturnRows(rows)

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^DELETE FROM "commands" WHERE "commands"\."command_uuid" = \$1`).
		WithArgs("stale-uuid-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	err = expireStaleCommands()

	assert.Equal(t, nil, err)
	if err := mockSpy.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %s", err)
	}
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

	postgresMock, mockSpy, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Fail to get postgres mock: %v", err)
	}
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	rows := sqlmock.NewRows([]string{"command_uuid", "status", "device_ud_id", "request_type", "updated_at"})
	wantCutoff := time.Now().Add(-5 * time.Minute)
	mockSpy.ExpectQuery(`^SELECT \* FROM "commands" WHERE status = \$1 AND updated_at < \$2 AND request_type NOT IN \(\$3,\$4\)`).
		WithArgs("", cutoffNear{wantCutoff}, "DeviceLock", "EraseDevice").
		WillReturnRows(rows)

	err = expireStaleCommands()

	assert.Equal(t, nil, err)
	if err := mockSpy.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %s", err)
	}
}

func TestResolveProfileCommandInQueue_NoneQueued(t *testing.T) {
	postgresMock, mockSpy, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Fail to get postgres mock: %v", err)
	}
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	mockSpy.ExpectQuery(`^SELECT \* FROM "commands" WHERE \(device_ud_id = \$1 AND request_type = \$2 AND identifier = \$3\) AND \(status = \$4 OR status = \$5\)`).
		WithArgs("1234-5678-123456", "InstallProfile", "com.example.profile", "", "NotNow").
		WillReturnRows(sqlmock.NewRows([]string{"command_uuid"}))

	device := types.Device{UDID: "1234-5678-123456"}
	inQueue, err := ResolveProfileCommandInQueue(device, "com.example.profile", "hash-a", "payload-a")

	assert.NoError(t, err)
	assert.False(t, inQueue, "no pending command should report not-in-queue")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestResolveProfileCommandInQueue_SameContentSkipsUpdate verifies a pending command whose
// content hash already matches the current profile is left untouched (true dedup, no write).
func TestResolveProfileCommandInQueue_SameContentSkipsUpdate(t *testing.T) {
	postgresMock, mockSpy, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Fail to get postgres mock: %v", err)
	}
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	rows := sqlmock.NewRows([]string{"command_uuid", "content_hash"}).
		AddRow("queued-uuid-1", "hash-a")
	mockSpy.ExpectQuery(`^SELECT \* FROM "commands" WHERE \(device_ud_id = \$1 AND request_type = \$2 AND identifier = \$3\) AND \(status = \$4 OR status = \$5\)`).
		WithArgs("1234-5678-123456", "InstallProfile", "com.example.profile", "", "NotNow").
		WillReturnRows(rows)

	device := types.Device{UDID: "1234-5678-123456"}
	inQueue, err := ResolveProfileCommandInQueue(device, "com.example.profile", "hash-a", "payload-a")

	assert.NoError(t, err)
	assert.True(t, inQueue)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestResolveProfileCommandInQueue_StaleContentRewritesInPlace verifies the race the dedup
// logic in PR #180 missed: a device goes offline while an InstallProfile command is queued,
// the profile's content changes before delivery, and the device later comes back online.
// Without this fix the stale command would be delivered as-is (or silently deduped forever).
// This asserts the pending row's payload/content_hash are rewritten with the fresh content
// instead, so the device gets the latest profile on its next checkin.
func TestResolveProfileCommandInQueue_StaleContentRewritesInPlace(t *testing.T) {
	postgresMock, mockSpy, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Fail to get postgres mock: %v", err)
	}
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	rows := sqlmock.NewRows([]string{"command_uuid", "content_hash"}).
		AddRow("queued-uuid-1", "hash-old")
	mockSpy.ExpectQuery(`^SELECT \* FROM "commands" WHERE \(device_ud_id = \$1 AND request_type = \$2 AND identifier = \$3\) AND \(status = \$4 OR status = \$5\)`).
		WithArgs("1234-5678-123456", "InstallProfile", "com.example.profile", "", "NotNow").
		WillReturnRows(rows)

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^UPDATE "commands" SET "content_hash"=\$1,"payload"=\$2,"updated_at"=\$3 WHERE command_uuid = \$4`).
		WithArgs("hash-new", "payload-new", sqlmock.AnyArg(), "queued-uuid-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	device := types.Device{UDID: "1234-5678-123456"}
	inQueue, err := ResolveProfileCommandInQueue(device, "com.example.profile", "hash-new", "payload-new")

	assert.NoError(t, err)
	assert.True(t, inQueue, "stale command is resolved in place, not re-enqueued as a duplicate")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestExpireStaleCommands_NoneStale(t *testing.T) {
	setupStaleCommandThresholdFlag(t)

	postgresMock, mockSpy, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Fail to get postgres mock: %v", err)
	}
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	rows := sqlmock.NewRows([]string{"command_uuid", "status", "device_ud_id", "request_type", "updated_at"})
	mockSpy.ExpectQuery(`^SELECT \* FROM "commands" WHERE status = \$1 AND updated_at < \$2 AND request_type NOT IN \(\$3,\$4\)`).
		WithArgs("", sqlmock.AnyArg(), "DeviceLock", "EraseDevice").
		WillReturnRows(rows)

	err = expireStaleCommands()

	assert.Equal(t, nil, err)
	if err := mockSpy.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %s", err)
	}
}
