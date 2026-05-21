package director

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testUDID = "ABCDEF12-3456-7890-ABCD-EF1234567890"

// SQL fragments matched against sqlmock's regex matcher. We intentionally match
// loosely so whitespace/formatting tweaks in the production query don't break tests.
const (
	acquireSQL = `UPDATE devices[\s\S]*run_initial_tasks_starttime = NOW\(\)[\s\S]*initial_tasks_run = false`
	releaseSQL = `UPDATE devices[\s\S]*run_initial_tasks_starttime = NULL`
)

func TestTryAcquireInitialTasksLease_Acquires(t *testing.T) {
	mock, teardown := setupMockDB(t)
	defer teardown()

	mock.ExpectExec(acquireSQL).
		WithArgs(testUDID).
		WillReturnResult(sqlmock.NewResult(0, 1)) // 1 row affected => acquired

	got, err := tryAcquireInitialTasksLease(testUDID)
	require.NoError(t, err)
	assert.True(t, got, "expected lease to be acquired when 1 row updated")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTryAcquireInitialTasksLease_DeniedWhenAnotherInFlight(t *testing.T) {
	mock, teardown := setupMockDB(t)
	defer teardown()

	mock.ExpectExec(acquireSQL).
		WithArgs(testUDID).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected => someone else holds it (or already complete)

	got, err := tryAcquireInitialTasksLease(testUDID)
	require.NoError(t, err)
	assert.False(t, got, "expected lease NOT acquired when 0 rows updated")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTryAcquireInitialTasksLease_DBError(t *testing.T) {
	mock, teardown := setupMockDB(t)
	defer teardown()

	mock.ExpectExec(acquireSQL).
		WithArgs(testUDID).
		WillReturnError(errors.New("connection reset"))

	got, err := tryAcquireInitialTasksLease(testUDID)
	require.Error(t, err)
	assert.False(t, got)
	assert.Contains(t, err.Error(), "tryAcquireInitialTasksLease")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestReleaseInitialTasksLease_ClearsTimestamp(t *testing.T) {
	mock, teardown := setupMockDB(t)
	defer teardown()

	mock.ExpectExec(releaseSQL).
		WithArgs(testUDID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	releaseInitialTasksLease(testUDID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestRunInitialTasks_EmptyUDIDShortCircuits ensures the empty-UDID guard fires
// before any DB work — sqlmock would flag any unexpected query.
func TestRunInitialTasks_EmptyUDIDShortCircuits(t *testing.T) {
	mock, teardown := setupMockDB(t)
	defer teardown()

	err := RunInitialTasks("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No Device UDID")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestRunInitialTasks_LeaseDeniedSkipsInnerFlow is the key behavioural test:
// when the CAS returns 0 rows affected, RunInitialTasks must return nil
// without invoking GetDevice / RequestAllDeviceInfo / InstallAllProfiles / etc.
// We assert this by registering ONLY the CAS expectation in sqlmock — any
// further DB call (which all the inner functions ultimately make) would
// trip "unexpected query" and fail the test.
func TestRunInitialTasks_LeaseDeniedSkipsInnerFlow(t *testing.T) {
	mock, teardown := setupMockDB(t)
	defer teardown()

	mock.ExpectExec(acquireSQL).
		WithArgs(testUDID).
		WillReturnResult(sqlmock.NewResult(0, 0)) // denied

	err := RunInitialTasks(testUDID)
	require.NoError(t, err, "lease-denied path must return nil, not an error")
	assert.NoError(t, mock.ExpectationsWereMet(),
		"no further DB queries should run when lease is not acquired")
}

// TestRunInitialTasks_AcquireErrorPropagates verifies a CAS failure
// is wrapped and returned, and does not silently swallow.
func TestRunInitialTasks_AcquireErrorPropagates(t *testing.T) {
	mock, teardown := setupMockDB(t)
	defer teardown()

	mock.ExpectExec(acquireSQL).
		WithArgs(testUDID).
		WillReturnError(errors.New("db down"))

	err := RunInitialTasks(testUDID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RunInitialTasks")
	assert.Contains(t, err.Error(), "db down")
	assert.NoError(t, mock.ExpectationsWereMet())
}
