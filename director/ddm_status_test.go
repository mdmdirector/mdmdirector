package director

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startMockKMFDDMStatus points the global client at a server whose status-values response
// carries the given management-status entries for enrollmentID. statusCode overrides the
// response code (200 unless set) so error paths can be exercised.
func startMockKMFDDMStatus(t *testing.T, enrollmentID string, values []ddm.StatusValue, statusCode int) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if statusCode != 0 && statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string][]ddm.StatusValue{enrollmentID: values})
	}))
	t.Cleanup(server.Close)

	ddm.InitClient(server.URL, "test-key")
}

func statusValue(path, timestamp string) ddm.StatusValue {
	return ddm.StatusValue{Path: path, Value: json.RawMessage(`"x"`), Timestamp: timestamp, StatusID: "sid"}
}

// serveDDMStatus routes a request through a mux router so {udid} is populated
func serveDDMStatus(t *testing.T, udid string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/device/"+udid+"/ddm/status", nil)

	router := mux.NewRouter()
	router.HandleFunc("/device/{udid}/ddm/status", DeviceDDMStatusHandler).Methods(http.MethodGet)
	router.ServeHTTP(rr, req)
	return rr
}

// A device that has reported management status values is enabled, and the handler surfaces
// the newest timestamp
func TestDeviceDDMStatusHandler_Enabled(t *testing.T) {
	startMockKMFDDMStatus(t, "udid-1", []ddm.StatusValue{
		statusValue(".StatusItems.management.client-capabilities.supported-payloads", "2026-08-31T10:00:00Z"),
		statusValue(".StatusItems.management.declarations.configurations", "2026-08-31T12:00:00Z"),
	}, http.StatusOK)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockGetDevice(mockSpy, "udid-1")

	rr := serveDDMStatus(t, "udid-1")

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, true, body["ddm_enabled"])
	assert.Equal(t, float64(2), body["status_count"])
	assert.Equal(t, "2026-08-31T12:00:00Z", body["last_status_at"])
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// A device that has reported nothing under the management tree is not enabled, and no
// timestamp is included
func TestDeviceDDMStatusHandler_NotEnabled(t *testing.T) {
	startMockKMFDDMStatus(t, "udid-1", nil, http.StatusOK)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockGetDevice(mockSpy, "udid-1")

	rr := serveDDMStatus(t, "udid-1")

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, false, body["ddm_enabled"])
	assert.Equal(t, float64(0), body["status_count"])
	_, hasLast := body["last_status_at"]
	assert.False(t, hasLast)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// An unknown device is a 404 and never reaches KMFDDM
func TestDeviceDDMStatusHandler_UnknownDevice(t *testing.T) {
	startMockKMFDDMStatus(t, "missing-udid", nil, http.StatusOK)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	expectDeviceNotFound(mockSpy, "missing-udid")

	rr := serveDDMStatus(t, "missing-udid")

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// A KMFDDM failure surfaces as a 500
func TestDeviceDDMStatusHandler_KMFDDMError(t *testing.T) {
	startMockKMFDDMStatus(t, "udid-1", nil, http.StatusInternalServerError)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockGetDevice(mockSpy, "udid-1")

	rr := serveDDMStatus(t, "udid-1")

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.NotContains(t, rr.Body.String(), "ddm_enabled")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// The client returns only the requested enrollment's slice out of KMFDDM's keyed response
func TestGetStatusValues_ReturnsRequestedEnrollment(t *testing.T) {
	startMockKMFDDMStatus(t, "udid-1", []ddm.StatusValue{
		statusValue(".StatusItems.management.client-capabilities", "2026-08-31T10:00:00Z"),
	}, http.StatusOK)

	client, err := ddm.Client()
	require.NoError(t, err)

	values, err := client.GetStatusValues("udid-1", ddmManagementStatusPrefix)
	require.NoError(t, err)
	require.Len(t, values, 1)
	assert.Equal(t, ".StatusItems.management.client-capabilities", values[0].Path)
}
