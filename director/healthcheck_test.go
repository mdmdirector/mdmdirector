package director

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/stretchr/testify/assert"
)

type fakePinger struct {
	err    error
	called bool
}

func (p *fakePinger) PingContext(_ context.Context) error {
	p.called = true
	return p.err
}

// withPinger swaps the package-level resolver for the duration of a test.
func withPinger(t *testing.T, p pinger, err error) {
	t.Helper()
	original := resolvePinger
	resolvePinger = func() (pinger, error) { return p, err }
	t.Cleanup(func() { resolvePinger = original })
}

func TestHealthCheckReachableDB(t *testing.T) {
	fake := &fakePinger{}
	withPinger(t, fake, nil)
	rec := httptest.NewRecorder()

	HealthCheck(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"UP"}`, rec.Body.String())
	assert.True(t, fake.called)
}

func TestHealthCheckUnreachableDB(t *testing.T) {
	withPinger(t, &fakePinger{err: errors.New("connection refused")}, nil)
	rec := httptest.NewRecorder()

	HealthCheck(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.JSONEq(t, `{"status":"DOWN"}`, rec.Body.String())
}

// /health is registered before db.Open() runs, so an unresolvable pool must
// report DOWN rather than panic on a nil db.DB.
func TestHealthCheckDBNotOpen(t *testing.T) {
	withPinger(t, nil, errors.New("database is not open"))
	rec := httptest.NewRecorder()

	HealthCheck(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.JSONEq(t, `{"status":"DOWN"}`, rec.Body.String())
}

// The default resolver must not dereference a nil db.DB. db.DB is set
// explicitly because other tests in this package assign it globally.
func TestDefaultResolvePingerWithoutOpenDB(t *testing.T) {
	original := db.DB
	db.DB = nil
	t.Cleanup(func() { db.DB = original })

	_, err := resolvePinger()

	assert.Error(t, err)
}
