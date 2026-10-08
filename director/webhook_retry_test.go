package director

import (
	"io"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	prometheustestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const staleRetryUDID = "1234-5678-123456"

// setupNoTxMockDB is a sqlmock-backed db.DB without gorm's default transaction, so each
// statement is one expectation and a failed one needs no rollback expectation.
func setupNoTxMockDB(t *testing.T) sqlmock.Sqlmock {
	t.Helper()
	oldDB := db.DB
	postgresMock, mockSpy, err := sqlmock.New()
	require.NoError(t, err)
	db.DB, err = gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	t.Cleanup(func() {
		db.DB = oldDB
		postgresMock.Close()
	})
	return mockSpy
}

func TestRetryOnStaleConnection(t *testing.T) {
	setBoolFlag(t, "prometheus", true)

	t.Run("a success runs once and counts nothing", func(t *testing.T) {
		recovered := prometheustestutil.ToFloat64(metrics.WebhookStepRetries("test_ok", "recovered"))
		calls := 0
		err := retryOnStaleConnection("test_ok", LogHolder{}, func() error {
			calls++
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, calls)
		assert.Equal(t, recovered, prometheustestutil.ToFloat64(metrics.WebhookStepRetries("test_ok", "recovered")))
	})

	t.Run("a stale connection is retried and counted as recovered", func(t *testing.T) {
		counter := metrics.WebhookStepRetries("test_recover", "recovered")
		before := prometheustestutil.ToFloat64(counter)
		calls := 0
		err := retryOnStaleConnection("test_recover", LogHolder{}, func() error {
			calls++
			if calls < 3 {
				return io.ErrUnexpectedEOF
			}
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 3, calls)
		assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))
	})

	t.Run("gives up after the attempt limit and counts it as failed", func(t *testing.T) {
		counter := metrics.WebhookStepRetries("test_fail", "failed")
		before := prometheustestutil.ToFloat64(counter)
		calls := 0
		err := retryOnStaleConnection("test_fail", LogHolder{}, func() error {
			calls++
			return io.ErrUnexpectedEOF
		})
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		assert.Equal(t, staleConnectionAttempts, calls)
		assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))
	})

	t.Run("other errors are not retried", func(t *testing.T) {
		calls := 0
		err := retryOnStaleConnection("test_other", LogHolder{}, func() error {
			calls++
			return errDBGoneAway
		})
		require.ErrorIs(t, err, errDBGoneAway)
		assert.Equal(t, 1, calls)
	})
}

// A CheckOut whose first statement hits a reset connection is not lost: ResetDevice runs
// again from the top and completes.
func TestHandleCheckinEvent_CheckOut_RetriesResetDeviceOnStaleConnection(t *testing.T) {
	utils.FlagProvider = mockFlagBuilder{false}
	setBoolFlag(t, "prometheus", false)
	mockSpy := setupNoTxMockDB(t)

	mockSpy.ExpectExec(`^DELETE FROM "commands"`).WillReturnError(io.ErrUnexpectedEOF)
	mockSpy.ExpectExec(`^DELETE FROM "commands"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mockSpy.ExpectExec(`^DELETE FROM "ddm_opt_ins"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mockSpy.ExpectExec(`^UPDATE "devices"`).WillReturnResult(sqlmock.NewResult(0, 1))

	err := handleCheckinEvent("mdm.CheckOut", &types.CheckinEvent{UDID: staleRetryUDID, RawPayload: []byte(testDevicePlist)})

	require.NoError(t, err)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// A failed lookup used to fall through and return an empty device with no error, so the
// handler acted on a blank device and nothing saw the failure to retry it.
func TestUpdateDevice_LookupFailureIsReturned(t *testing.T) {
	mockSpy := setupNoTxMockDB(t)
	mockSpy.ExpectQuery(`^SELECT \* FROM "devices" WHERE ud_id = \$1`).WillReturnError(io.ErrUnexpectedEOF)

	_, err := UpdateDevice(types.Device{UDID: staleRetryUDID})

	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// The retried UpdateDevice re-reads the row on a fresh connection and carries on.
func TestUpdateDeviceRetrying_RecoversFromStaleLookup(t *testing.T) {
	setBoolFlag(t, "prometheus", false)
	mockSpy := setupNoTxMockDB(t)
	mockSpy.ExpectQuery(`^SELECT \* FROM "devices" WHERE ud_id = \$1`).WillReturnError(io.ErrUnexpectedEOF)
	mockSpy.ExpectQuery(`^SELECT \* FROM "devices" WHERE ud_id = \$1`).
		WillReturnRows(sqlmock.NewRows([]string{"ud_id", "build_version"}).AddRow(staleRetryUDID, "OLD"))
	mockSpy.ExpectQuery(`^SELECT \* FROM "devices" WHERE ud_id = \$1`).
		WillReturnRows(sqlmock.NewRows([]string{"ud_id", "build_version"}).AddRow(staleRetryUDID, "OLD"))
	mockSpy.ExpectExec(`^UPDATE "devices"`).WillReturnResult(sqlmock.NewResult(0, 1))

	device, err := updateDeviceRetrying(types.Device{UDID: staleRetryUDID, BuildVersion: "NEW"})

	require.NoError(t, err)
	assert.Equal(t, staleRetryUDID, device.UDID)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
