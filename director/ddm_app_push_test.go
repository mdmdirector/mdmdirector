package director

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAppUUID    = "550e8400-e29b-41d4-a716-446655440000"
	testPackageID  = "com.example.DEVICE-UDID-1234.package.550e8400-e29b-41d4-a716-446655440000"
	testActiPackID = "com.example.DEVICE-UDID-1234.package_activation.550e8400-e29b-41d4-a716-446655440000"
)

func newTestApp(manifestURL string) types.DeviceInstallApplication {
	return types.DeviceInstallApplication{
		ID:          uuid.MustParse(testAppUUID),
		ManifestURL: manifestURL,
	}
}

func TestPushApplicationViaDDM_AllNew(t *testing.T) {
	server, requests, _ := newMockKMFDDM(t)
	defer server.Close()

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestApp("https://example.com/app.plist")

	err := PushApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.NoError(t, err)

	// When declarations are new (304), no touch calls should be made.
	// Expected sequence: PUT decl (package), PUT decl (activation), PUT set-decl (package),
	// PUT set-decl (activation), PUT enrollment-set (nonotify=true), POST notify
	assert.Len(t, *requests, 6)

	reqs := *requests

	// Step 1: PUT Package declaration
	assert.Equal(t, "PUT", reqs[0].Method)
	assert.Equal(t, "/v1/declarations", reqs[0].Path)
	assert.Contains(t, reqs[0].Query, "nonotify=true")
	var packageDecl ddm.Declaration
	err = json.Unmarshal([]byte(reqs[0].Body), &packageDecl)
	require.NoError(t, err)
	assert.Equal(t, testPackageID, packageDecl.Identifier)
	assert.Equal(t, ddm.TypePackage, packageDecl.Type)

	// Step 2: PUT ActivationSimple declaration
	assert.Equal(t, "PUT", reqs[1].Method)
	assert.Equal(t, "/v1/declarations", reqs[1].Path)
	assert.Contains(t, reqs[1].Query, "nonotify=true")
	var activationDecl ddm.Declaration
	err = json.Unmarshal([]byte(reqs[1].Body), &activationDecl)
	require.NoError(t, err)
	assert.Equal(t, testActiPackID, activationDecl.Identifier)
	assert.Equal(t, ddm.TypeActivationSimple, activationDecl.Type)

	// Step 3: PUT set-declaration (package)
	assert.Equal(t, "PUT", reqs[2].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[2].Path)
	assert.Contains(t, reqs[2].Query, "declaration="+testPackageID)
	assert.Contains(t, reqs[2].Query, "nonotify=true")

	// Step 4: PUT set-declaration (activation)
	assert.Equal(t, "PUT", reqs[3].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[3].Path)
	assert.Contains(t, reqs[3].Query, "declaration="+testActiPackID)
	assert.Contains(t, reqs[3].Query, "nonotify=true")

	// Step 5: PUT enrollment-set (nonotify=true)
	assert.Equal(t, "PUT", reqs[4].Method)
	assert.Equal(t, "/v1/enrollment-sets/DEVICE-UDID-1234", reqs[4].Path)
	assert.Contains(t, reqs[4].Query, "set=DEVICE-UDID-1234")
	assert.Contains(t, reqs[4].Query, "nonotify=true")

	// Step 6: POST notify - triggers DDM sync unconditionally
	assert.Equal(t, "POST", reqs[5].Method)
	assert.Equal(t, "/v1/notify", reqs[5].Path)
	assert.Contains(t, reqs[5].Query, "id=DEVICE-UDID-1234")
}

func TestPushApplicationViaDDM_UnchangedDeclarations_TouchCalled(t *testing.T) {
	server, requests, statusOverrides := newMockKMFDDM(t)
	defer server.Close()

	// Override PUT declarations to return 304 (unchanged)
	statusOverrides["PUT /v1/declarations"] = http.StatusNotModified

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestApp("https://example.com/app.plist")

	err := PushApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.NoError(t, err)

	// When declarations are unchanged (304), touch calls should be made.
	// Expected: PUT decl (package), POST touch (package), PUT decl (activation),
	// POST touch (activation), PUT set-decl (package), PUT set-decl (activation),
	// PUT enrollment-set (nonotify=true), POST notify
	assert.Len(t, *requests, 8)

	reqs := *requests

	// Step 1: PUT Package declaration (returns 204 = unchanged)
	assert.Equal(t, "PUT", reqs[0].Method)
	assert.Equal(t, "/v1/declarations", reqs[0].Path)

	// Step 1b: POST touch for Package
	assert.Equal(t, "POST", reqs[1].Method)
	assert.Equal(t, "/v1/declarations/"+testPackageID+"/touch", reqs[1].Path)
	assert.Contains(t, reqs[1].Query, "nonotify=true")

	// Step 2: PUT ActivationSimple declaration (returns 204 = unchanged)
	assert.Equal(t, "PUT", reqs[2].Method)
	assert.Equal(t, "/v1/declarations", reqs[2].Path)

	// Step 2b: POST touch for ActivationSimple
	assert.Equal(t, "POST", reqs[3].Method)
	assert.Equal(t, "/v1/declarations/"+testActiPackID+"/touch", reqs[3].Path)
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

	// Step 6: POST notify
	assert.Equal(t, "POST", reqs[7].Method)
	assert.Equal(t, "/v1/notify", reqs[7].Path)
	assert.Contains(t, reqs[7].Query, "id=DEVICE-UDID-1234")
}

func TestPushApplicationViaDDM_ActivationReferencesPackageDeclaration(t *testing.T) {
	server, requests, _ := newMockKMFDDM(t)
	defer server.Close()

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestApp("https://example.com/app.plist")

	err := PushApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.NoError(t, err)

	reqs := *requests
	// Parse the activation declaration to verify it references the package declaration
	var activationDecl struct {
		Identifier string `json:"Identifier"`
		Payload    struct {
			StandardConfigurations []string `json:"StandardConfigurations"`
		} `json:"Payload"`
	}
	err = json.Unmarshal([]byte(reqs[1].Body), &activationDecl)
	require.NoError(t, err)

	assert.Len(t, activationDecl.Payload.StandardConfigurations, 1)
	assert.Equal(t, testPackageID, activationDecl.Payload.StandardConfigurations[0])
}

func TestPushApplicationViaDDM_PackagePayloadContainsManifestURL(t *testing.T) {
	server, requests, _ := newMockKMFDDM(t)
	defer server.Close()

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestApp("https://example.com/myapp.plist")

	err := PushApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.NoError(t, err)

	reqs := *requests
	// Parse the package declaration to verify it contains the manifest URL
	// and the InstallBehavior.Install=Required key required for auto-install.
	var payload struct {
		Payload struct {
			ManifestURL     string `json:"ManifestURL"`
			InstallBehavior struct {
				Install string `json:"Install"`
			} `json:"InstallBehavior"`
		} `json:"Payload"`
	}
	err = json.Unmarshal([]byte(reqs[0].Body), &payload)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/myapp.plist", payload.Payload.ManifestURL)
	assert.Equal(t, "Required", payload.Payload.InstallBehavior.Install)
}

func TestPushApplicationViaDDM_PutDeclarationError(t *testing.T) {
	server, _, statusOverrides := newMockKMFDDM(t)
	defer server.Close()

	statusOverrides["PUT /v1/declarations"] = http.StatusInternalServerError

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestApp("https://example.com/app.plist")

	err := PushApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PUT Package declaration")
}

func TestPushApplicationViaDDM_TouchError(t *testing.T) {
	server, _, statusOverrides := newMockKMFDDM(t)
	defer server.Close()

	// Declarations return 304 (unchanged) so touch is called
	statusOverrides["PUT /v1/declarations"] = http.StatusNotModified
	// Touch returns 500
	statusOverrides["POST /v1/declarations/"+testPackageID+"/touch"] = http.StatusInternalServerError

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestApp("https://example.com/app.plist")

	err := PushApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "touch Package declaration")
}

func TestPushApplicationViaDDM_NotifyError(t *testing.T) {
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
	app := newTestApp("https://example.com/app.plist")

	err := PushApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notify enrollment")
}

const (
	testSharedAppUUID  = "660e8400-e29b-41d4-a716-446655440001"
	testSharedPkgID    = "com.example.DEVICE-UDID-1234.package.660e8400-e29b-41d4-a716-446655440001"
	testSharedActPkgID = "com.example.DEVICE-UDID-1234.package_activation.660e8400-e29b-41d4-a716-446655440001"
)

func newTestSharedApp() types.SharedInstallApplication {
	return types.SharedInstallApplication{
		ID:          uuid.MustParse(testSharedAppUUID),
		ManifestURL: "https://example.com/app.plist",
	}
}

func TestDeleteSharedInstallApplicationViaDDM_Success(t *testing.T) {
	server, requests, _ := newMockKMFDDM(t)
	defer server.Close()

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestSharedApp()

	err := DeleteSharedInstallApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.NoError(t, err)

	// Expected sequence:
	// 1. DELETE set-declaration (package)
	// 2. DELETE set-declaration (activation)
	// 3. DELETE declaration (package)
	// 4. DELETE declaration (activation)
	// 5. POST notify
	assert.Len(t, *requests, 5)
	reqs := *requests

	assert.Equal(t, "DELETE", reqs[0].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[0].Path)
	assert.Contains(t, reqs[0].Query, "declaration="+testSharedPkgID)
	assert.Contains(t, reqs[0].Query, "nonotify=true")

	assert.Equal(t, "DELETE", reqs[1].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[1].Path)
	assert.Contains(t, reqs[1].Query, "declaration="+testSharedActPkgID)
	assert.Contains(t, reqs[1].Query, "nonotify=true")

	assert.Equal(t, "DELETE", reqs[2].Method)
	assert.Equal(t, "/v1/declarations/"+testSharedPkgID, reqs[2].Path)
	assert.Contains(t, reqs[2].Query, "nonotify=true")

	assert.Equal(t, "DELETE", reqs[3].Method)
	assert.Equal(t, "/v1/declarations/"+testSharedActPkgID, reqs[3].Path)
	assert.Contains(t, reqs[3].Query, "nonotify=true")

	assert.Equal(t, "POST", reqs[4].Method)
	assert.Equal(t, "/v1/notify", reqs[4].Path)
	assert.Contains(t, reqs[4].Query, "id=DEVICE-UDID-1234")
}

func TestDeleteSharedInstallApplicationViaDDM_AlreadyGone(t *testing.T) {
	// 404 responses (declarations already deleted) should not be errors
	server, requests, statusOverrides := newMockKMFDDM(t)
	defer server.Close()

	statusOverrides["DELETE /v1/set-declarations/DEVICE-UDID-1234"] = http.StatusNotFound
	statusOverrides["DELETE /v1/declarations/"+testSharedPkgID] = http.StatusNotFound
	statusOverrides["DELETE /v1/declarations/"+testSharedActPkgID] = http.StatusNotFound

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestSharedApp()

	err := DeleteSharedInstallApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.NoError(t, err)

	// Notify should still be called
	reqs := *requests
	assert.Equal(t, "POST", reqs[len(reqs)-1].Method)
	assert.Equal(t, "/v1/notify", reqs[len(reqs)-1].Path)
}

func TestDeleteSharedInstallApplicationViaDDM_DeleteSetDeclarationError(t *testing.T) {
	server, _, statusOverrides := newMockKMFDDM(t)
	defer server.Close()

	statusOverrides["DELETE /v1/set-declarations/DEVICE-UDID-1234"] = http.StatusInternalServerError

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestSharedApp()

	err := DeleteSharedInstallApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove package set-declaration")
}

func TestDeleteSharedInstallApplicationViaDDM_DeleteDeclarationError(t *testing.T) {
	server, _, statusOverrides := newMockKMFDDM(t)
	defer server.Close()

	statusOverrides["DELETE /v1/declarations/"+testSharedPkgID] = http.StatusInternalServerError

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestSharedApp()

	err := DeleteSharedInstallApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete package declaration")
}

func TestDeleteSharedInstallApplicationViaDDM_NotifyError(t *testing.T) {
	server, _, statusOverrides := newMockKMFDDM(t)
	defer server.Close()

	statusOverrides["POST /v1/notify"] = http.StatusInternalServerError

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestSharedApp()

	err := DeleteSharedInstallApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notify enrollment")
}
