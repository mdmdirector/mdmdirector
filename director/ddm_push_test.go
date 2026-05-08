package director

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	flag.String("ddm-declaration-prefix", "com.example", "DDM declaration prefix")
	os.Exit(m.Run())
}

// requestLog records HTTP requests made to the mock KMFDDM server.
type requestLog struct {
	Method string
	Path   string
	Query  string
	Body   string
}

// newMockKMFDDM creates a mock KMFDDM server
func newMockKMFDDM(t *testing.T) (*httptest.Server, *[]requestLog, map[string]int) {
	t.Helper()
	var requests []requestLog
	// statusOverrides allows tests to override response codes for specific paths
	statusOverrides := make(map[string]int)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes := make([]byte, 0)
		if r.Body != nil {
			buf := make([]byte, 1024)
			for {
				n, err := r.Body.Read(buf)
				if n > 0 {
					bodyBytes = append(bodyBytes, buf[:n]...)
				}
				if err != nil {
					break
				}
			}
		}

		req := requestLog{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Body:   string(bodyBytes),
		}
		requests = append(requests, req)

		// Check for status override
		key := r.Method + " " + r.URL.Path
		if status, ok := statusOverrides[key]; ok {
			w.WriteHeader(status)
			return
		}

		// Default responses
		switch {
		case r.Method == "PUT" && r.URL.Path == "/v1/declarations":
			// 204 = changed/new in KMFDDM
			w.WriteHeader(http.StatusNoContent)
		default:
			// 204 for touch, set-declarations, enrollment-sets
			w.WriteHeader(http.StatusNoContent)
		}
	}))

	return server, &requests, statusOverrides
}

// setupDDMPushTest wires up a mock KMFDDM server, mock DB, and mock NanoMDM server
// needed by PushProfileViaDDM and DeleteProfileViaDDM (which call SendCommand internally).
// Returns the KMFDDM client, request log, status overrides, and cleanup func.
const ddmTestUDID = "DEVICE-UDID-1234"

func setupDDMPushTest(t *testing.T) (*ddm.KMFDDMClient, *[]requestLog, map[string]int, func()) {
	t.Helper()

	setupNanoMDMFlag(t)

	kmfddmServer, requests, statusOverrides := newMockKMFDDM(t)

	// Mock DB for GetDevice lookup inside SendCommand
	mockSpy, dbCleanup := setupMockDB(t)
	mockGetDevice(mockSpy, ddmTestUDID)
	mockCreateCommand(mockSpy)

	// Mock NanoMDM server that returns a successful enqueue response
	nanoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := mdm.APIResponse{
			CommandUUID: "ddm-cmd-uuid",
			RequestType: "DeclarativeManagement",
			Status: map[string]mdm.EnrollmentStatus{
				ddmTestUDID: {PushResult: "success"},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	mdm.InitClient(nanoServer.URL, "test-api-key")

	client := ddm.NewKMFDDMClient(kmfddmServer.URL, "testapikey")

	cleanup := func() {
		kmfddmServer.Close()
		nanoServer.Close()
		dbCleanup()
		mdm.InitClient("", "")
	}

	return client, requests, statusOverrides, cleanup
}

func TestPushProfileViaDDM_AllNew(t *testing.T) {
	client, requests, _, cleanup := setupDDMPushTest(t)
	defer cleanup()

	err := PushProfileViaDDM(client, ddmTestUDID, "com.example.wifi", "https://mdm.example.com")
	require.NoError(t, err)

	// When declarations are new/changed (204), no touch calls should be made.
	// Expected kmfddm sequence: PUT decl (legacy), PUT decl (activation), PUT set-decl (legacy),
	// PUT set-decl (activation), PUT enrollment-set (nonotify=true)
	// Step 6 (DeclarativeManagement enqueue) goes to nanomdm, not captured here.
	assert.Len(t, *requests, 5)

	reqs := *requests

	// Step 1: PUT LegacyProfile declaration
	assert.Equal(t, "PUT", reqs[0].Method)
	assert.Equal(t, "/v1/declarations", reqs[0].Path)
	assert.Contains(t, reqs[0].Query, "nonotify=true")
	var legacyDecl ddm.Declaration
	err = json.Unmarshal([]byte(reqs[0].Body), &legacyDecl)
	require.NoError(t, err)
	assert.Equal(t, "com.example.DEVICE-UDID-1234.legacy_profile.com.example.wifi", legacyDecl.Identifier)
	assert.Equal(t, ddm.TypeLegacyProfile, legacyDecl.Type)

	// Step 2: PUT ActivationSimple declaration
	assert.Equal(t, "PUT", reqs[1].Method)
	assert.Equal(t, "/v1/declarations", reqs[1].Path)
	assert.Contains(t, reqs[1].Query, "nonotify=true")
	var activationDecl ddm.Declaration
	err = json.Unmarshal([]byte(reqs[1].Body), &activationDecl)
	require.NoError(t, err)
	assert.Equal(t, "com.example.DEVICE-UDID-1234.legacy_profile_activation.com.example.wifi", activationDecl.Identifier)
	assert.Equal(t, ddm.TypeActivationSimple, activationDecl.Type)

	// Step 3: PUT set-declaration (legacy)
	assert.Equal(t, "PUT", reqs[2].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[2].Path)
	assert.Contains(t, reqs[2].Query, "declaration=com.example.DEVICE-UDID-1234.legacy_profile.com.example.wifi")
	assert.Contains(t, reqs[2].Query, "nonotify=true")

	// Step 4: PUT set-declaration (activation)
	assert.Equal(t, "PUT", reqs[3].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[3].Path)
	assert.Contains(t, reqs[3].Query, "declaration=com.example.DEVICE-UDID-1234.legacy_profile_activation.com.example.wifi")
	assert.Contains(t, reqs[3].Query, "nonotify=true")

	// Step 5: PUT enrollment-set (nonotify=true - DeclarativeManagement enqueued directly in step 6)
	assert.Equal(t, "PUT", reqs[4].Method)
	assert.Equal(t, "/v1/enrollment-sets/DEVICE-UDID-1234", reqs[4].Path)
	assert.Contains(t, reqs[4].Query, "set=DEVICE-UDID-1234")
	assert.Contains(t, reqs[4].Query, "nonotify=true")
}

func TestPushProfileViaDDM_UnchangedDeclarations_TouchCalled(t *testing.T) {
	client, requests, statusOverrides, cleanup := setupDDMPushTest(t)
	defer cleanup()

	// Override PUT declarations to return 304 (unchanged)
	statusOverrides["PUT /v1/declarations"] = http.StatusNotModified

	err := PushProfileViaDDM(client, ddmTestUDID, "com.example.wifi", "https://mdm.example.com")
	require.NoError(t, err)

	// When declarations are unchanged (304), touch calls should be made.
	// Expected kmfddm: PUT decl (legacy), POST touch (legacy), PUT decl (activation),
	// POST touch (activation), PUT set-decl (legacy), PUT set-decl (activation),
	// PUT enrollment-set (nonotify=true)
	assert.Len(t, *requests, 7)

	reqs := *requests

	// Step 1: PUT LegacyProfile declaration (returns 304 = unchanged)
	assert.Equal(t, "PUT", reqs[0].Method)
	assert.Equal(t, "/v1/declarations", reqs[0].Path)

	// Step 1b: POST touch for LegacyProfile
	assert.Equal(t, "POST", reqs[1].Method)
	assert.Equal(t, "/v1/declarations/com.example.DEVICE-UDID-1234.legacy_profile.com.example.wifi/touch", reqs[1].Path)
	assert.Contains(t, reqs[1].Query, "nonotify=true")

	// Step 2: PUT ActivationSimple declaration (returns 304 = unchanged)
	assert.Equal(t, "PUT", reqs[2].Method)
	assert.Equal(t, "/v1/declarations", reqs[2].Path)

	// Step 2b: POST touch for ActivationSimple
	assert.Equal(t, "POST", reqs[3].Method)
	assert.Equal(t, "/v1/declarations/com.example.DEVICE-UDID-1234.legacy_profile_activation.com.example.wifi/touch", reqs[3].Path)
	assert.Contains(t, reqs[3].Query, "nonotify=true")

	// Step 3-4: PUT set-declarations
	assert.Equal(t, "PUT", reqs[4].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[4].Path)

	assert.Equal(t, "PUT", reqs[5].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[5].Path)

	// Step 5: PUT enrollment-set (nonotify=true)
	assert.Equal(t, "PUT", reqs[6].Method)
	assert.Equal(t, "/v1/enrollment-sets/DEVICE-UDID-1234", reqs[6].Path)
	assert.Contains(t, reqs[6].Query, "nonotify=true")
}

func TestPushProfileViaDDM_ActivationReferencesLegacyDeclaration(t *testing.T) {
	client, requests, _, cleanup := setupDDMPushTest(t)
	defer cleanup()

	err := PushProfileViaDDM(client, ddmTestUDID, "com.example.wifi", "https://mdm.example.com")
	require.NoError(t, err)

	reqs := *requests
	// Parse the activation declaration to verify it references the legacy declaration
	var activationDecl struct {
		Identifier string `json:"Identifier"`
		Payload    struct {
			StandardConfigurations []string `json:"StandardConfigurations"`
		} `json:"Payload"`
	}
	err = json.Unmarshal([]byte(reqs[1].Body), &activationDecl)
	require.NoError(t, err)

	expectedLegacyID := "com.example.DEVICE-UDID-1234.legacy_profile.com.example.wifi"
	assert.Len(t, activationDecl.Payload.StandardConfigurations, 1)
	assert.Equal(t, expectedLegacyID, activationDecl.Payload.StandardConfigurations[0])
}

func TestPushProfileViaDDM_PutDeclarationError(t *testing.T) {
	_, _, statusOverrides := newMockKMFDDM(t)
	kmfddmServer, _, _ := newMockKMFDDM(t)
	defer kmfddmServer.Close()
	statusOverrides["PUT /v1/declarations"] = http.StatusInternalServerError

	// Rebuild server with error override - use a fresh mock that returns 500
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errServer.Close()

	client := ddm.NewKMFDDMClient(errServer.URL, "testapikey")

	err := PushProfileViaDDM(client, ddmTestUDID, "com.example.wifi", "https://mdm.example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PUT LegacyProfile declaration")
}

func TestPushProfileViaDDM_TouchError(t *testing.T) {
	_, _, statusOverrides := newMockKMFDDM(t)

	// Build a server where PUT /v1/declarations returns 304 but touch returns 500
	touchErrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && r.URL.Path == "/v1/declarations" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if r.Method == "POST" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer touchErrServer.Close()
	_ = statusOverrides

	client := ddm.NewKMFDDMClient(touchErrServer.URL, "testapikey")

	err := PushProfileViaDDM(client, ddmTestUDID, "com.example.wifi", "https://mdm.example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "touch LegacyProfile declaration")
}

func TestDeleteProfileViaDDM_Success(t *testing.T) {
	client, requests, _, cleanup := setupDDMPushTest(t)
	defer cleanup()

	err := DeleteProfileViaDDM(client, ddmTestUDID, "com.example.wifi")
	require.NoError(t, err)

	// Expected kmfddm: DELETE set-decl (legacy), DELETE set-decl (activation),
	// DELETE decl (legacy), DELETE decl (activation), PUT enrollment-set (nonotify=true)
	// Step 6 (DeclarativeManagement) goes to nanomdm.
	assert.Len(t, *requests, 5)

	reqs := *requests

	// Step 1: DELETE set-declaration for LegacyProfile
	assert.Equal(t, "DELETE", reqs[0].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[0].Path)
	assert.Contains(t, reqs[0].Query, "declaration=com.example.DEVICE-UDID-1234.legacy_profile.com.example.wifi")
	assert.Contains(t, reqs[0].Query, "nonotify=true")

	// Step 2: DELETE set-declaration for ActivationSimple
	assert.Equal(t, "DELETE", reqs[1].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[1].Path)
	assert.Contains(t, reqs[1].Query, "declaration=com.example.DEVICE-UDID-1234.legacy_profile_activation.com.example.wifi")
	assert.Contains(t, reqs[1].Query, "nonotify=true")

	// Step 3: DELETE declaration (legacy)
	assert.Equal(t, "DELETE", reqs[2].Method)
	assert.Equal(t, "/v1/declarations/com.example.DEVICE-UDID-1234.legacy_profile.com.example.wifi", reqs[2].Path)
	assert.Contains(t, reqs[2].Query, "nonotify=true")

	// Step 4: DELETE declaration (activation)
	assert.Equal(t, "DELETE", reqs[3].Method)
	assert.Equal(t, "/v1/declarations/com.example.DEVICE-UDID-1234.legacy_profile_activation.com.example.wifi", reqs[3].Path)
	assert.Contains(t, reqs[3].Query, "nonotify=true")

	// Step 5: PUT enrollment-set (nonotify=true - DeclarativeManagement enqueued directly in step 6)
	assert.Equal(t, "PUT", reqs[4].Method)
	assert.Equal(t, "/v1/enrollment-sets/DEVICE-UDID-1234", reqs[4].Path)
	assert.Contains(t, reqs[4].Query, "set=DEVICE-UDID-1234")
	assert.Contains(t, reqs[4].Query, "nonotify=true")
}

func TestDeleteProfileViaDDM_DeleteSetDeclarationError(t *testing.T) {
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer errServer.Close()

	client := ddm.NewKMFDDMClient(errServer.URL, "testapikey")

	err := DeleteProfileViaDDM(client, ddmTestUDID, "com.example.wifi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DELETE set-declaration (legacy)")
}
