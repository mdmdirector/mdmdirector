package director

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	flag.String("ddm-declaration-prefix", "com.example", "DDM declaration prefix")
	flag.String("ddm-set-prefix", "com.example.set", "DDM set prefix")
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

		// Default: 204 for all endpoints
		w.WriteHeader(http.StatusNoContent)
	}))

	return server, &requests, statusOverrides
}

// setupDDMPushTest wires up a mock KMFDDM server for PushProfileViaDDM and DeleteProfileViaDDM.
// All steps including notify hit the KMFDDM mock and are captured in the request log.
const ddmTestUDID = "DEVICE-UDID-1234"

func setupDDMPushTest(t *testing.T) (*ddm.KMFDDMClient, *[]requestLog, map[string]int, func()) {
	t.Helper()

	kmfddmServer, requests, statusOverrides := newMockKMFDDM(t)
	client := ddm.NewKMFDDMClient(kmfddmServer.URL, "testapikey")

	cleanup := func() {
		kmfddmServer.Close()
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
	// PUT set-decl (activation), PUT enrollment-set (nonotify=true), POST notify
	assert.Len(t, *requests, 6)

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
	assert.Equal(t, "/v1/set-declarations/com.example.set.DEVICE-UDID-1234", reqs[2].Path)
	assert.Contains(t, reqs[2].Query, "declaration=com.example.DEVICE-UDID-1234.legacy_profile.com.example.wifi")
	assert.Contains(t, reqs[2].Query, "nonotify=true")

	// Step 4: PUT set-declaration (activation)
	assert.Equal(t, "PUT", reqs[3].Method)
	assert.Equal(t, "/v1/set-declarations/com.example.set.DEVICE-UDID-1234", reqs[3].Path)
	assert.Contains(t, reqs[3].Query, "declaration=com.example.DEVICE-UDID-1234.legacy_profile_activation.com.example.wifi")
	assert.Contains(t, reqs[3].Query, "nonotify=true")

	// Step 5: PUT enrollment-set (nonotify=true)
	assert.Equal(t, "PUT", reqs[4].Method)
	assert.Equal(t, "/v1/enrollment-sets/DEVICE-UDID-1234", reqs[4].Path)
	assert.Contains(t, reqs[4].Query, "set=com.example.set.DEVICE-UDID-1234")
	assert.Contains(t, reqs[4].Query, "nonotify=true")

	// Step 6: POST notify - triggers DDM sync unconditionally
	assert.Equal(t, "POST", reqs[5].Method)
	assert.Equal(t, "/v1/notify", reqs[5].Path)
	assert.Contains(t, reqs[5].Query, "id=DEVICE-UDID-1234")
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
	// PUT enrollment-set (nonotify=true), POST notify
	assert.Len(t, *requests, 8)

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
	assert.Equal(t, "/v1/set-declarations/com.example.set.DEVICE-UDID-1234", reqs[4].Path)

	assert.Equal(t, "PUT", reqs[5].Method)
	assert.Equal(t, "/v1/set-declarations/com.example.set.DEVICE-UDID-1234", reqs[5].Path)

	// Step 5: PUT enrollment-set (nonotify=true)
	assert.Equal(t, "PUT", reqs[6].Method)
	assert.Equal(t, "/v1/enrollment-sets/DEVICE-UDID-1234", reqs[6].Path)
	assert.Contains(t, reqs[6].Query, "nonotify=true")

	// Step 6: POST notify
	assert.Equal(t, "POST", reqs[7].Method)
	assert.Equal(t, "/v1/notify", reqs[7].Path)
	assert.Contains(t, reqs[7].Query, "id=DEVICE-UDID-1234")
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

	client := ddm.NewKMFDDMClient(touchErrServer.URL, "testapikey")

	err := PushProfileViaDDM(client, ddmTestUDID, "com.example.wifi", "https://mdm.example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "touch LegacyProfile declaration")
}

func TestPushProfileViaDDM_NotifyError(t *testing.T) {
	// Build a server that succeeds for all steps but returns 500 for POST /v1/notify
	notifyErrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v1/notify" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer notifyErrServer.Close()

	client := ddm.NewKMFDDMClient(notifyErrServer.URL, "testapikey")

	err := PushProfileViaDDM(client, ddmTestUDID, "com.example.wifi", "https://mdm.example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notify enrollment")
}

func TestDeleteProfileViaDDM_Success(t *testing.T) {
	client, requests, _, cleanup := setupDDMPushTest(t)
	defer cleanup()

	err := DeleteProfileViaDDM(client, ddmTestUDID, "com.example.wifi")
	require.NoError(t, err)

	// Expected kmfddm: DELETE set-decl (legacy), DELETE set-decl (activation),
	// DELETE decl (legacy), DELETE decl (activation), PUT enrollment-set (nonotify=true), POST notify
	assert.Len(t, *requests, 6)

	reqs := *requests

	// Step 1: DELETE set-declaration for LegacyProfile
	assert.Equal(t, "DELETE", reqs[0].Method)
	assert.Equal(t, "/v1/set-declarations/com.example.set.DEVICE-UDID-1234", reqs[0].Path)
	assert.Contains(t, reqs[0].Query, "declaration=com.example.DEVICE-UDID-1234.legacy_profile.com.example.wifi")
	assert.Contains(t, reqs[0].Query, "nonotify=true")

	// Step 2: DELETE set-declaration for ActivationSimple
	assert.Equal(t, "DELETE", reqs[1].Method)
	assert.Equal(t, "/v1/set-declarations/com.example.set.DEVICE-UDID-1234", reqs[1].Path)
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

	// Step 5: PUT enrollment-set (nonotify=true)
	assert.Equal(t, "PUT", reqs[4].Method)
	assert.Equal(t, "/v1/enrollment-sets/DEVICE-UDID-1234", reqs[4].Path)
	assert.Contains(t, reqs[4].Query, "set=com.example.set.DEVICE-UDID-1234")
	assert.Contains(t, reqs[4].Query, "nonotify=true")

	// Step 6: POST notify - triggers DDM sync unconditionally
	assert.Equal(t, "POST", reqs[5].Method)
	assert.Equal(t, "/v1/notify", reqs[5].Path)
	assert.Contains(t, reqs[5].Query, "id=DEVICE-UDID-1234")
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

// stageProfileViaDDM does steps 1-5 only: the caller notifies.
func TestStageProfileViaDDM_DoesNotNotify(t *testing.T) {
	client, requests, _, cleanup := setupDDMPushTest(t)
	defer cleanup()

	err := stageProfileViaDDM(client, ddmTestUDID, "com.example.profile", "https://nanomdm.example.com")
	require.NoError(t, err)

	reqs := *requests
	require.Len(t, reqs, 5)
	for _, r := range reqs {
		assert.NotEqual(t, "/v1/notify", r.Path)
		assert.Contains(t, r.Query, "nonotify=true")
	}
}

// A device staged for many profiles is notified once, in the order devices were added.
func TestDDMNotifyBatch_NotifiesEachDeviceOnce(t *testing.T) {
	client, requests, _, cleanup := setupDDMPushTest(t)
	defer cleanup()

	a := types.Device{UDID: "UDID-A"}
	b := types.Device{UDID: "UDID-B"}
	var batch ddmNotifyBatch
	for _, d := range []types.Device{a, b, a, a, b} {
		batch.Add(d)
	}
	require.Empty(t, batch.Flush(client))

	reqs := *requests
	require.Len(t, reqs, 2)
	for i, udid := range []string{"UDID-A", "UDID-B"} {
		assert.Equal(t, "POST", reqs[i].Method)
		assert.Equal(t, "/v1/notify", reqs[i].Path)
		assert.Equal(t, "id="+udid, reqs[i].Query)
	}

	// Flush empties the batch.
	require.Empty(t, batch.Flush(client))
	assert.Len(t, *requests, 2)
}

// One failed notify doesn't stop the rest.
func TestDDMNotifyBatch_NotifyErrorContinues(t *testing.T) {
	client, requests, statusOverrides, cleanup := setupDDMPushTest(t)
	defer cleanup()
	statusOverrides["POST /v1/notify"] = http.StatusInternalServerError

	var batch ddmNotifyBatch
	batch.Add(types.Device{UDID: "UDID-A"})
	batch.Add(types.Device{UDID: "UDID-B"})
	errs := batch.Flush(client)

	assert.Len(t, errs, 2)
	assert.Len(t, *requests, 2)
	assert.Contains(t, errs[0].Error(), "UDID-A")
}

// setupDDMGlobalPushTest points the global KMFDDM client at a request-logging mock, for the
// multi-profile functions that call ddm.Client() themselves.
func setupDDMGlobalPushTest(t *testing.T) (*[]requestLog, map[string]int) {
	t.Helper()
	// The multi-profile functions read the prometheus flag
	if flag.Lookup("prometheus") == nil {
		flag.Bool("prometheus", false, "Enable prometheus metrics")
	}
	server, requests, statusOverrides := newMockKMFDDM(t)
	t.Cleanup(server.Close)
	ddm.InitClient(server.URL, "testapikey")
	return requests, statusOverrides
}

// notifiedUDIDs returns the id= of every POST /v1/notify, in order.
func notifiedUDIDs(requests []requestLog) []string {
	var udids []string
	for _, r := range requests {
		if r.Method == "POST" && r.Path == "/v1/notify" {
			udids = append(udids, strings.TrimPrefix(r.Query, "id="))
		}
	}
	return udids
}

func testDeviceProfiles(ids ...string) []types.DeviceProfile {
	profiles := make([]types.DeviceProfile, 0, len(ids))
	for _, id := range ids {
		profiles = append(profiles, types.DeviceProfile{PayloadIdentifier: id})
	}
	return profiles
}

// stageProfileDeleteViaDDM does steps 1-5 only: the caller notifies.
func TestStageProfileDeleteViaDDM_DoesNotNotify(t *testing.T) {
	client, requests, _, cleanup := setupDDMPushTest(t)
	defer cleanup()

	err := stageProfileDeleteViaDDM(client, ddmTestUDID, "com.example.wifi")
	require.NoError(t, err)

	reqs := *requests
	require.Len(t, reqs, 5)
	for _, r := range reqs {
		assert.NotEqual(t, "/v1/notify", r.Path)
		assert.Contains(t, r.Query, "nonotify=true")
	}
}

// Deleting several profiles notifies each device once, after all its deletes are staged.
func TestDeleteDeviceProfilesViaDDM_NotifiesEachDeviceOnce(t *testing.T) {
	tests := []struct {
		name  string
		udids []string
	}{
		{name: "one device", udids: []string{"UDID-A"}},
		{name: "two devices", udids: []string{"UDID-A", "UDID-B"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requests, _ := setupDDMGlobalPushTest(t)

			devices := make([]types.Device, 0, len(tc.udids))
			for _, udid := range tc.udids {
				devices = append(devices, types.Device{UDID: udid})
			}
			profiles := testDeviceProfiles("com.example.a", "com.example.b", "com.example.c")

			require.NoError(t, DeleteDeviceProfilesViaDDM(devices, profiles))

			reqs := *requests
			// 5 staged calls per profile per device, then one notify per device
			assert.Len(t, reqs, 5*len(profiles)*len(devices)+len(devices))
			assert.Equal(t, tc.udids, notifiedUDIDs(reqs))
			for _, r := range reqs[:len(reqs)-len(devices)] {
				assert.NotEqual(t, "/v1/notify", r.Path)
			}
		})
	}
}

// A device whose deletes all failed has nothing to sync, so it isn't notified.
func TestDeleteDeviceProfilesViaDDM_AllStagesFail_NoNotify(t *testing.T) {
	requests, statusOverrides := setupDDMGlobalPushTest(t)
	statusOverrides["DELETE /v1/set-declarations/com.example.set.UDID-A"] = http.StatusInternalServerError

	devices := []types.Device{{UDID: "UDID-A"}}
	require.NoError(t, DeleteDeviceProfilesViaDDM(devices, testDeviceProfiles("com.example.a", "com.example.b")))

	assert.Empty(t, notifiedUDIDs(*requests))
}

// One failed delete doesn't stop the others, and the device is still notified for them.
func TestDeleteDeviceProfilesViaDDM_SomeStagesFail_NotifiesOnce(t *testing.T) {
	requests, statusOverrides := setupDDMGlobalPushTest(t)
	statusOverrides["DELETE /v1/declarations/com.example.UDID-A.legacy_profile.com.example.a"] = http.StatusInternalServerError

	devices := []types.Device{{UDID: "UDID-A"}}
	require.NoError(t, DeleteDeviceProfilesViaDDM(devices, testDeviceProfiles("com.example.a", "com.example.b")))

	assert.Equal(t, []string{"UDID-A"}, notifiedUDIDs(*requests))
}

// Shared deletes batch the same way, and skip a device with a device-specific version.
func TestDeleteSharedProfilesViaDDM_NotifiesEachDeviceOnce(t *testing.T) {
	requests, _ := setupDDMGlobalPushTest(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	profiles := []types.SharedProfile{
		{PayloadIdentifier: "com.example.a"},
		{PayloadIdentifier: "com.example.b"},
	}
	// getDeviceSpecificProfileUDIDs: UDID-B has its own com.example.b
	mockSpy.ExpectQuery(`SELECT "device_ud_id" FROM "device_profiles" WHERE payload_identifier = \$1`).
		WithArgs("com.example.a").
		WillReturnRows(sqlmock.NewRows([]string{"device_ud_id"}))
	mockSpy.ExpectQuery(`SELECT "device_ud_id" FROM "device_profiles" WHERE payload_identifier = \$1`).
		WithArgs("com.example.b").
		WillReturnRows(sqlmock.NewRows([]string{"device_ud_id"}).AddRow("UDID-B"))

	devices := []types.Device{{UDID: "UDID-A"}, {UDID: "UDID-B"}}
	require.NoError(t, DeleteSharedProfilesViaDDM(devices, profiles))
	require.NoError(t, mockSpy.ExpectationsWereMet())

	reqs := *requests
	// A: 2 profiles, B: 1 profile, 5 staged calls each, then one notify per device
	assert.Len(t, reqs, 5*3+2)
	assert.Equal(t, []string{"UDID-A", "UDID-B"}, notifiedUDIDs(reqs))
}

func TestDeleteSharedProfilesViaDDM_AllStagesFail_NoNotify(t *testing.T) {
	requests, statusOverrides := setupDDMGlobalPushTest(t)
	statusOverrides["DELETE /v1/set-declarations/com.example.set.UDID-A"] = http.StatusInternalServerError
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(`SELECT "device_ud_id" FROM "device_profiles" WHERE payload_identifier = \$1`).
		WithArgs("com.example.a").
		WillReturnRows(sqlmock.NewRows([]string{"device_ud_id"}))

	devices := []types.Device{{UDID: "UDID-A"}}
	require.NoError(t, DeleteSharedProfilesViaDDM(devices, []types.SharedProfile{{PayloadIdentifier: "com.example.a"}}))
	require.NoError(t, mockSpy.ExpectationsWereMet())

	assert.Empty(t, notifiedUDIDs(*requests))
}
