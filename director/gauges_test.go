package director

import (
	"io"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

// TestSetProfilesGaugeRetriesStaleConnection verifies a gauge COUNT that fails on a
// connection the mesh reset is retried once and the gauge is still set.
func TestSetProfilesGaugeRetriesStaleConnection(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	countQuery := `SELECT count\(\*\) FROM "shared_profiles" WHERE installed = \$1`
	mockSpy.ExpectQuery(countQuery).WithArgs(true).WillReturnError(io.ErrUnexpectedEOF)
	mockSpy.ExpectQuery(countQuery).WithArgs(true).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
	mockSpy.ExpectQuery(countQuery).WithArgs(false).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	setProfilesGauge("shared", &types.SharedProfile{})

	assert.NoError(t, mockSpy.ExpectationsWereMet())
	assert.Equal(t, float64(7), testutil.ToFloat64(metrics.ProfilesTotal("shared", "true")))
	assert.Equal(t, float64(2), testutil.ToFloat64(metrics.ProfilesTotal("shared", "false")))
}
