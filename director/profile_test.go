package director

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestVerifyMDMProfiles(t *testing.T) {
	mockDb, mockSpy, err := sqlmock.New()
	defer mockDb.Close()

	DB, err := gorm.Open("postgres", mockDb)
	db.DB = DB

	mockSpy.ExpectQuery(`^SELECT \* FROM "device_profiles" WHERE \(device_ud_id = \$1 AND installed = true\)`).WithArgs(
		"1234-5678-123456",
	).WillReturnRows(&sqlmock.Rows{})

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(
		`^UPDATE "profile_lists" SET "device_ud_id" = \$1, "has_removal_passcode" = \$2, "is_encrypted" = \$3, "is_managed" = \$4, "payload_description" = \$5, "payload_display_name" = \$6, "payload_identifier" = \$7, "payload_organization" = \$8, "payload_removal_disallowed" = \$9, "payload_uuid" = \$10, "payload_version" = \$11, "full_payload" = \$12 WHERE "profile_lists"\."id" = \$13`,
	).WithArgs(
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
	// ).WillReturnRows(&sqlmock.Rows{})
	).WillReturnResult(sqlmock.NewErrorResult(errors.New("database has rejected this request")))

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(
		`^UPDATE "profile_lists" SET "device_ud_id" = \$1 WHERE \("id" NOT IN \(\$2\)\) AND \("device_ud_id" = \$3\)`,
	).WithArgs(
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
	).WillReturnResult(sqlmock.NewErrorResult(errors.New("database has rejected this request")))

	profileListData := types.ProfileListData{
		ProfileList: []types.ProfileList{
			types.ProfileList{
				ID: uuid.New(),
			},
		},
	}
	device := types.Device{
		SerialNumber: "C02ABCDEFGH",
		UDID:         "1234-5678-123456",
	}
	err = VerifyMDMProfiles(profileListData, device)

	assert.NotEmpty(t, err)
	assert.Equal(t, "VerifyMDMProfiles: Cannot replace Profile List: database has rejected this request", err.Error())
}
