package director

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/types"
	prometheustestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestSetProfilesGauge_OneGroupedQuery(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(`^SELECT installed, count\(\*\) AS count FROM "device_profiles" WHERE installed IS NOT NULL GROUP BY "installed"$`).
		WillReturnRows(sqlmock.NewRows([]string{"installed", "count"}).AddRow(true, 120).AddRow(false, 7))

	setProfilesGauge("device", &types.DeviceProfile{})

	assert.Equal(t, float64(120), prometheustestutil.ToFloat64(metrics.ProfilesTotal("device", "true")))
	assert.Equal(t, float64(7), prometheustestutil.ToFloat64(metrics.ProfilesTotal("device", "false")))
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestSetProfilesGauge_MissingStateIsZero(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	metrics.ProfilesTotal("shared", "false").Set(5)
	mockSpy.ExpectQuery(`^SELECT installed, count\(\*\) AS count FROM "shared_profiles"`).
		WillReturnRows(sqlmock.NewRows([]string{"installed", "count"}).AddRow(true, 9))

	setProfilesGauge("shared", &types.SharedProfile{})

	assert.Equal(t, float64(9), prometheustestutil.ToFloat64(metrics.ProfilesTotal("shared", "true")))
	assert.Equal(t, float64(0), prometheustestutil.ToFloat64(metrics.ProfilesTotal("shared", "false")))
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
