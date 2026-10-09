package director

import (
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Regression test for the IsSupervised-clobber bug: UpdateDevice must not
// write the DeviceInformation-only boolean columns (is_supervised et al) by
// itself. A Device decoded from a checkin/generic-acknowledge payload has
// those fields at their Go zero value (false), not a real reading from the
// device, so a blind write there would clobber a correct prior value set by
// an actual DeviceInformation response.
//
// If UpdateDevice regresses to calling UpdateDeviceBools internally again,
// this test fails because sqlmock rejects the extra, unexpected UPDATE.
func TestUpdateDevice_DoesNotWriteDeviceInformationBools(t *testing.T) {
	postgresMock, mockSpy, _ := sqlmock.New()
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	rows := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"ud_id"}).AddRow("1234-5678-123456")
	}
	// UpdateDevice's lookup of the existing row.
	mockSpy.ExpectQuery(`^SELECT \* FROM "devices" WHERE ud_id = \$1`).
		WillReturnRows(rows())
	// Assign().FirstOrCreate()'s internal lookup, then its UPDATE.
	mockSpy.ExpectQuery(`^SELECT \* FROM "devices" WHERE ud_id = \$1 AND "devices"\."ud_id" = \$2`).
		WillReturnRows(rows())
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^UPDATE "devices"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mockSpy.ExpectCommit()

	// Zero-value IsSupervised etc, as if decoded from a checkin payload that
	// never carries these fields.
	newDevice := types.Device{UDID: "1234-5678-123456"}

	_, err := UpdateDevice(newDevice)

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
