package director

import (
	"bytes"
	"flag"
	"net/http"
	"net/http/httptest"
	"testing"

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
	// These need to be set due to global variable referencing
	flag.String("micromdmurl", server.URL, "MicroMDM Server URL")
	flag.String("micromdmapikey", "", "MicroMDM Server API Key")
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
