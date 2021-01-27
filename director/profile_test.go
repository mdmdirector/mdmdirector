package director

import (
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestVerifyMDMProfiles(t *testing.T) {
	postgresMock, mockSpy, _ := sqlmock.New()
	defer postgresMock.Close()

	DB, _ := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	db.DB = DB

	mockSpy.ExpectQuery(`^SELECT \* FROM "device_profiles" WHERE device_ud_id = \$1 AND installed = true`).WithArgs(
		"1234-5678-123456",
	).WillReturnRows(&sqlmock.Rows{})

	// mockSpy.ExpectBegin()
	// mockSpy.ExpectExec(
	// 	`^UPDATE "profile_lists" SET "device_ud_id" = \$1, "has_removal_passcode" = \$2, "is_encrypted" = \$3, "is_managed" = \$4, "payload_description" = \$5, "payload_display_name" = \$6, "payload_identifier" = \$7, "payload_organization" = \$8, "payload_removal_disallowed" = \$9, "payload_uuid" = \$10, "payload_version" = \$11, "full_payload" = \$12 WHERE "profile_lists"\."id" = \$13`,
	// ).WithArgs(
	// 	sqlmock.AnyArg(),
	// 	sqlmock.AnyArg(),
	// 	sqlmock.AnyArg(),
	// 	sqlmock.AnyArg(),
	// 	sqlmock.AnyArg(),
	// 	sqlmock.AnyArg(),
	// 	sqlmock.AnyArg(),
	// 	sqlmock.AnyArg(),
	// 	sqlmock.AnyArg(),
	// 	sqlmock.AnyArg(),
	// 	sqlmock.AnyArg(),
	// 	sqlmock.AnyArg(),
	// 	sqlmock.AnyArg(),
	// // ).WillReturnRows(&sqlmock.Rows{})
	// ).WillReturnError(errors.New("database has rejected this request"))

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(
		`^UPDATE "profile_lists" SET "device_ud_id"=\$1 WHERE "profile_lists"\."id" <> \$2 AND "profile_lists"\."device_ud_id" = \$3`,
	).WithArgs(
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
	).WillReturnError(errors.New("database has rejected this request"))

	profileListData := types.ProfileListData{
		ProfileList: []types.ProfileList{
			{
				ID: uuid.New(),
			},
		},
	}
	device := types.Device{
		SerialNumber: "C02ABCDEFGH",
		UDID:         "1234-5678-123456",
	}

	fmt.Println(profileListData)
	err := VerifyMDMProfiles(profileListData, device)

	assert.NotEmpty(t, err)
	assert.Equal(t, "VerifyMDMProfiles: Cannot replace Profile List: Profile must have a PayloadUUID", err.Error())
}
