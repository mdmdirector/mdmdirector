package director

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
)

// POST /profile for a UDID this director has never seen is a client error: 404, not 500
func TestPostProfileHandler_UnknownUDID(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	expectDeviceNotFound(mockSpy, "missing-udid")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/profile",
		strings.NewReader(`{"udids":["missing-udid"],"profiles":[],"metadata":true,"push_now":true}`))
	PostProfileHandler(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "device not found")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// serveSingleDevice routes through a mux router so {udid} is populated
func serveSingleDevice(t *testing.T, udid string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/device/"+udid, nil)

	router := mux.NewRouter()
	router.HandleFunc("/device/{udid}", SingleDeviceHandler).Methods(http.MethodGet)
	router.ServeHTTP(rr, req)
	return rr
}

// GET /device/{udid} for an unknown UDID is 404 and writes nothing else
func TestSingleDeviceHandler_UnknownUDID(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	expectDeviceNotFound(mockSpy, "missing-udid")

	rr := serveSingleDevice(t, "missing-udid")

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Empty(t, rr.Body.String())
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// A real DB failure on the lookup is still a 500, and the handler stops there instead of
// falling through to SingleDeviceOutput with an empty device
func TestSingleDeviceHandler_DBError(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1`).
		WithArgs("any-udid").
		WillReturnError(errors.New("connection refused"))

	rr := serveSingleDevice(t, "any-udid")

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Empty(t, rr.Body.String())
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
