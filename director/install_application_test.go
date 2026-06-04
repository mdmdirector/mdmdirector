package director

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSharedAppUUID   = "660e8400-e29b-41d4-a716-446655440001"
	testSharedPkgID     = "com.example.DEVICE-UDID-1234.package.660e8400-e29b-41d4-a716-446655440001"
	testSharedActPkgID  = "com.example.DEVICE-UDID-1234.package_activation.660e8400-e29b-41d4-a716-446655440001"
)

func newTestSharedApp(manifestURL string) types.SharedInstallApplication {
	return types.SharedInstallApplication{
		ID:          uuid.MustParse(testSharedAppUUID),
		ManifestURL: manifestURL,
	}
}

func TestDeleteSharedInstallApplicationViaDDM_Success(t *testing.T) {
	server, requests, _ := newMockKMFDDM(t)
	defer server.Close()

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestSharedApp("https://example.com/app.plist")

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
	app := newTestSharedApp("https://example.com/app.plist")

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
	app := newTestSharedApp("https://example.com/app.plist")

	err := DeleteSharedInstallApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove package set-declaration")
}

func TestDeleteSharedInstallApplicationViaDDM_DeleteDeclarationError(t *testing.T) {
	server, _, statusOverrides := newMockKMFDDM(t)
	defer server.Close()

	statusOverrides["DELETE /v1/declarations/"+testSharedPkgID] = http.StatusInternalServerError

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestSharedApp("https://example.com/app.plist")

	err := DeleteSharedInstallApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete package declaration")
}

func TestDeleteSharedInstallApplicationViaDDM_NotifyError(t *testing.T) {
	server, _, statusOverrides := newMockKMFDDM(t)
	defer server.Close()

	statusOverrides["POST /v1/notify"] = http.StatusInternalServerError

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")
	app := newTestSharedApp("https://example.com/app.plist")

	err := DeleteSharedInstallApplicationViaDDM(client, "DEVICE-UDID-1234", app)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notify enrollment")
}
