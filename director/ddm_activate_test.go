package director

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ddmCallCounts records how many times each KMFDDM endpoint the activate path uses was hit
type ddmCallCounts struct {
	enrollmentSet atomic.Int32
	notify        atomic.Int32
}

// startMockKMFDDM stands up a KMFDDM stand-in and points the global client at it. notifyStatus
// is the status code returned by POST /v1/notify (204 for success, 5xx to force a failure).
func startMockKMFDDM(t *testing.T, notifyStatus int) *ddmCallCounts {
	t.Helper()

	// observeDDMNotify reads the prometheus flag; register it so the lookup never panics
	if flag.Lookup("prometheus") == nil {
		flag.Bool("prometheus", false, "Enable prometheus metrics")
	}

	counts := &ddmCallCounts{}
	handler := http.NewServeMux()
	handler.HandleFunc("/v1/enrollment-sets/", func(w http.ResponseWriter, r *http.Request) {
		counts.enrollmentSet.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	handler.HandleFunc("/v1/notify", func(w http.ResponseWriter, r *http.Request) {
		counts.notify.Add(1)
		w.WriteHeader(notifyStatus)
	})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	ddm.InitClient(server.URL, "test-key")
	return counts
}

// --- activateDDM ---

func TestActivateDDM_Success(t *testing.T) {
	counts := startMockKMFDDM(t, http.StatusNoContent)
	client, err := ddm.Client()
	require.NoError(t, err)

	require.NoError(t, activateDDM(client, "udid-1"))
	assert.Equal(t, int32(1), counts.enrollmentSet.Load())
	assert.Equal(t, int32(1), counts.notify.Load())
}

// A non-204 from notify surfaces as an error, and enrollment-set was still attempted first
func TestActivateDDM_NotifyFails(t *testing.T) {
	counts := startMockKMFDDM(t, http.StatusInternalServerError)
	client, err := ddm.Client()
	require.NoError(t, err)

	err = activateDDM(client, "udid-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notify enrollment")
	assert.Equal(t, int32(1), counts.enrollmentSet.Load())
	assert.Equal(t, int32(1), counts.notify.Load())
}

// --- activateDevices (fleet loop) ---

func TestActivateDevices_ActivatesEachAndSkipsEmptyUDID(t *testing.T) {
	counts := startMockKMFDDM(t, http.StatusNoContent)
	client, err := ddm.Client()
	require.NoError(t, err)

	devices := testDevices("udid-1", "udid-2")
	devices = append(devices, testDevices("")...) // empty UDID must be skipped

	activated, err := activateDevices(client, devices)
	require.NoError(t, err)
	assert.Equal(t, 2, activated)
	// One enrollment-set + one notify per real device; the empty UDID made no calls
	assert.Equal(t, int32(2), counts.enrollmentSet.Load())
	assert.Equal(t, int32(2), counts.notify.Load())
}

// A per-device failure is collected but does not abort the rest
func TestActivateDevices_CollectsErrorsAndContinues(t *testing.T) {
	counts := startMockKMFDDM(t, http.StatusInternalServerError) // every notify fails
	client, err := ddm.Client()
	require.NoError(t, err)

	activated, err := activateDevices(client, testDevices("udid-1", "udid-2"))
	require.Error(t, err)
	assert.Equal(t, 0, activated)
	assert.Len(t, errorMessages(err), 2)
	assert.Equal(t, int32(2), counts.notify.Load())
}

// --- ActivateDeviceDDMHandler ---

// serveActivate routes a request through a mux router so {udid} is populated
func serveActivate(t *testing.T, udid string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/device/"+udid+"/ddm/activate", nil)

	router := mux.NewRouter()
	router.HandleFunc("/device/{udid}/ddm/activate", ActivateDeviceDDMHandler).Methods(http.MethodPost)
	router.ServeHTTP(rr, req)
	return rr
}

func TestActivateDeviceDDMHandler_Success(t *testing.T) {
	counts := startMockKMFDDM(t, http.StatusNoContent)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockGetDevice(mockSpy, "udid-1")

	rr := serveActivate(t, "udid-1")

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "udid-1", body["device_udid"])
	assert.Equal(t, true, body["activated"])
	assert.Equal(t, int32(1), counts.notify.Load())
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// An unknown device is a 404 and reaches no KMFDDM endpoint
func TestActivateDeviceDDMHandler_UnknownDevice(t *testing.T) {
	counts := startMockKMFDDM(t, http.StatusNoContent)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	expectDeviceNotFound(mockSpy, "missing-udid")

	rr := serveActivate(t, "missing-udid")

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Equal(t, int32(0), counts.notify.Load())
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

// A KMFDDM failure after the device is found is a 500, not a partial success
func TestActivateDeviceDDMHandler_NotifyFails(t *testing.T) {
	startMockKMFDDM(t, http.StatusInternalServerError)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockGetDevice(mockSpy, "udid-1")

	rr := serveActivate(t, "udid-1")

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.NotContains(t, rr.Body.String(), "activated")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
