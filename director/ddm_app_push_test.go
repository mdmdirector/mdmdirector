package director

import (
	"encoding/json"
	"net/http"
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
	// PUT set-decl (activation), PUT enrollment-set
	assert.Len(t, *requests, 5)

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

	// Step 5: PUT enrollment-set (noNotify=false to trigger sync)
	assert.Equal(t, "PUT", reqs[4].Method)
	assert.Equal(t, "/v1/enrollment-sets/DEVICE-UDID-1234", reqs[4].Path)
	assert.Contains(t, reqs[4].Query, "set=DEVICE-UDID-1234")
	assert.NotContains(t, reqs[4].Query, "nonotify=true")
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
	// PUT enrollment-set
	assert.Len(t, *requests, 7)

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

	// Step 5: PUT enrollment-set
	assert.Equal(t, "PUT", reqs[6].Method)
	assert.Equal(t, "/v1/enrollment-sets/DEVICE-UDID-1234", reqs[6].Path)
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
	var payload struct {
		Payload struct {
			ManifestURL string `json:"ManifestURL"`
		} `json:"Payload"`
	}
	err = json.Unmarshal([]byte(reqs[0].Body), &payload)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/myapp.plist", payload.Payload.ManifestURL)
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
