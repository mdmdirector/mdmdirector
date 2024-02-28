package director

import (
	"crypto/x509"
	"encoding/pem"
	"flag"
	"testing"

	log "github.com/sirupsen/logrus"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/micromdm/go4/env"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

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

	// profileListData := types.ProfileListData{
	// 	ProfileList: []types.ProfileList{
	// 		{
	// 			ID: uuid.New(),
	// 		},
	// 	},
	// }
	// device := types.Device{
	// 	SerialNumber: "C02ABCDEFGH",
	// 	UDID:         "1234-5678-123456",
	// }

	// fmt.Println(profileListData)
	// err := VerifyMDMProfiles(profileListData, device)

	// assert.NotEmpty(t, err)
	// assert.Equal(t, "VerifyMDMProfiles: Cannot replace Profile List: Profile must have a PayloadUUID", err.Error())
}

const testSignerCert = `-----BEGIN CERTIFICATE-----
MIIDAzCCAeugAwIBAgIUA16SQdriz/Q2EFqL0m95W/gLOpkwDQYJKoZIhvcNAQEL
BQAwETEPMA0GA1UEAwwGU2lnbmVyMB4XDTI0MDIyODE2MzEyMFoXDTM0MDIyNTE2
MzEyMFowETEPMA0GA1UEAwwGU2lnbmVyMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEA0t6UyfLzFv83zYjrRdk/I0S+vsesJ02YE6TmMNR9DddnmORaMI1d
heiH7iZBoS6oLJUR3y09vH4sTj6vSEHo+Ei2g5nbl5DbNq5I0irCuuMJMD7hEOOF
fUSua5LRWmLWwYuqCimrVgcN9sdS/3g/Pzg0AE+GFlm7E/A0u3XQyh72p4u5KHvM
gH7DBcPJWxfBAO5u+zCNRo6nskYgTaXGzdtIMu1LrNXiguk3RORXpxhWTakOg+Ot
Y8SMhtPmcxtorHiLp0FsyQTmp+jp53VAG3G5EJ+OGNyNYYLkCPL2xXwdaKqXoQYJ
4FxwIJFpNBaABZAM1SF4p/VdDYgmtd85mwIDAQABo1MwUTAdBgNVHQ4EFgQUbnno
xn31NRijsusfnWPw8U7UFO0wHwYDVR0jBBgwFoAUbnnoxn31NRijsusfnWPw8U7U
FO0wDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAyX6q3xJeWdFu
3CZzAdFViacHovZYk+pZ9RxaAmOTNQLq0iTcodpsGLDyahFqdefVbVJjUwxGkfhg
nxpTaUwXC/WzaewR6E+dok6f/9L95UuqEo7z11m+gAX88mTvWquzXBGwjp7ZWSFc
GPFewfk17bvCAKY7YTsdP1NvegdoUe8jEBKNVqqSQc/miExY0dzM7wo0GecLA7A2
ITf22GCzFvCOaQI3ttr70HcI7opfcGKgkHDuefyduosZJh6tzVrzzQ4Eyb9KPskU
fiW4Gx8qTKtBIUUOyWZXDJ+HXI7yGnv/sxSHlTwNPtJPNtBJGT/NihHcl7SxtXZT
ELpQB3KUIQ==
-----END CERTIFICATE-----`

func TestValidateProfileInProfileList(t *testing.T) {
	// disable logging for test
	log.SetLevel(log.PanicLevel)
	defer log.SetLevel(log.InfoLevel)

	// set up global sign flag
	var Sign bool
	flag.BoolVar(
		&Sign,
		"sign",
		env.Bool("SIGN", false),
		"Sign profiles prior to sending to MicroMDM.",
	)

	// decode test signer cert
	p, _ := pem.Decode([]byte(testSignerCert))
	signer, err := x509.ParseCertificate(p.Bytes)
	require.NoError(t, err, "could not parse test certificate")

	type test struct {
		name           string
		profile        ProfileForVerification
		profileList    []types.ProfileList
		signCheck      bool
		installed      bool
		needsReinstall bool
	}

	tests := []test{
		{
			name: "Needs Install",
			profile: ProfileForVerification{
				PayloadUUID:       "1234-567",
				PayloadIdentifier: "com.example.profile",
				HashedPayloadUUID: "5432-765",
				Installed:         true,
			},
			profileList:    []types.ProfileList{},
			installed:      false,
			needsReinstall: true,
		},
		{
			name: "Needs Install Cert",
			profile: ProfileForVerification{
				PayloadUUID:       "1234-567",
				PayloadIdentifier: "com.example.profile",
				HashedPayloadUUID: "5432-765",
				Installed:         true,
			},
			profileList:    []types.ProfileList{},
			signCheck:      true,
			installed:      false,
			needsReinstall: true,
		},
		{
			name: "Already Installed",
			profile: ProfileForVerification{
				PayloadUUID:       "1234-567",
				PayloadIdentifier: "com.example.profile",
				HashedPayloadUUID: "5432-765",
				Installed:         true,
			},
			profileList: []types.ProfileList{
				{
					PayloadUUID:       "5432-765",
					PayloadIdentifier: "com.example.profile",
				},
			},
			installed:      true,
			needsReinstall: false,
		},
		{
			name: "Already Installed Signed",
			profile: ProfileForVerification{
				PayloadUUID:       "1234-567",
				PayloadIdentifier: "com.example.profile",
				HashedPayloadUUID: "5432-765",
				Installed:         true,
			},
			profileList: []types.ProfileList{
				{
					PayloadUUID:        "5432-765",
					PayloadIdentifier:  "com.example.profile",
					SignerCertificates: [][]byte{p.Bytes},
				},
			},
			signCheck:      true,
			installed:      true,
			needsReinstall: false,
		},
		{
			name: "Needs Reinstall",
			profile: ProfileForVerification{
				PayloadUUID:       "1234-567",
				PayloadIdentifier: "com.example.profile",
				HashedPayloadUUID: "5432-765",
				Installed:         true,
			},
			profileList: []types.ProfileList{
				{
					PayloadUUID:       "5432-765",
					PayloadIdentifier: "com.example.profile",
				},
			},
			signCheck:      true,
			installed:      true,
			needsReinstall: true,
		},
		{
			name: "Needs Removal",
			profile: ProfileForVerification{
				PayloadUUID:       "1234-567",
				PayloadIdentifier: "com.example.profile",
				HashedPayloadUUID: "5432-765",
				Installed:         false,
			},
			profileList: []types.ProfileList{
				{
					PayloadIdentifier: "com.example.profile",
				},
			},
			installed:      true,
			needsReinstall: false,
		},
		{
			name: "Needs Removal Signed",
			profile: ProfileForVerification{
				PayloadUUID:       "1234-567",
				PayloadIdentifier: "com.example.profile",
				HashedPayloadUUID: "5432-765",
				Installed:         false,
			},
			profileList: []types.ProfileList{
				{
					PayloadIdentifier: "com.example.profile",
				},
			},
			signCheck:      true,
			installed:      true,
			needsReinstall: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			Sign = test.signCheck
			var s *x509.Certificate
			if Sign {
				s = signer
			}
			install, needsReinstall, err := validateProfileInProfileList(test.profile, test.profileList, s)
			require.NoError(t, err, "unexpected error")
			require.Equal(t, test.installed, install, "unexpected install status")
			require.Equal(t, test.needsReinstall, needsReinstall, "unexpected needsReinstall status")
		})
	}
}
