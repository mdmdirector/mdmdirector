package director

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			// 304 = changed/new in KMFDDM
			w.WriteHeader(http.StatusNotModified)
		default:
			// 204 for touch, set-declarations, enrollment-sets
			w.WriteHeader(http.StatusNoContent)
		}
	}))

	return server, &requests, statusOverrides
}

func TestPushProfileViaDDM_AllNew(t *testing.T) {
	server, requests, _ := newMockKMFDDM(t)
	defer server.Close()

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")

	err := PushProfileViaDDM(client, "DEVICE-UDID-1234", "com.example.wifi", "https://mdm.example.com")
	require.NoError(t, err)

	// When declarations are new (304), no touch calls should be made.
	// Expected sequence: PUT decl (legacy), PUT decl (activation), PUT set-decl (legacy),
	// PUT set-decl (activation), PUT enrollment-set
	assert.Len(t, *requests, 5)

	reqs := *requests

	// Step 1: PUT LegacyProfile declaration
	assert.Equal(t, "PUT", reqs[0].Method)
	assert.Equal(t, "/v1/declarations", reqs[0].Path)
	assert.Contains(t, reqs[0].Query, "nonotify=true")
	var legacyDecl ddm.Declaration
	err = json.Unmarshal([]byte(reqs[0].Body), &legacyDecl)
	require.NoError(t, err)
	assert.Equal(t, "biz.airbnb.DEVICE-UDID-1234.legacy_profile.com.example.wifi", legacyDecl.Identifier)
	assert.Equal(t, ddm.TypeLegacyProfile, legacyDecl.Type)

	// Step 2: PUT ActivationSimple declaration
	assert.Equal(t, "PUT", reqs[1].Method)
	assert.Equal(t, "/v1/declarations", reqs[1].Path)
	assert.Contains(t, reqs[1].Query, "nonotify=true")
	var activationDecl ddm.Declaration
	err = json.Unmarshal([]byte(reqs[1].Body), &activationDecl)
	require.NoError(t, err)
	assert.Equal(t, "biz.airbnb.DEVICE-UDID-1234.legacy_profile_activation.com.example.wifi", activationDecl.Identifier)
	assert.Equal(t, ddm.TypeActivationSimple, activationDecl.Type)

	// Step 3: PUT set-declaration (legacy)
	assert.Equal(t, "PUT", reqs[2].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[2].Path)
	assert.Contains(t, reqs[2].Query, "declaration=biz.airbnb.DEVICE-UDID-1234.legacy_profile.com.example.wifi")
	assert.Contains(t, reqs[2].Query, "nonotify=true")

	// Step 4: PUT set-declaration (activation)
	assert.Equal(t, "PUT", reqs[3].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[3].Path)
	assert.Contains(t, reqs[3].Query, "declaration=biz.airbnb.DEVICE-UDID-1234.legacy_profile_activation.com.example.wifi")
	assert.Contains(t, reqs[3].Query, "nonotify=true")

	// Step 5: PUT enrollment-set (noNotify=false to trigger sync)
	assert.Equal(t, "PUT", reqs[4].Method)
	assert.Equal(t, "/v1/enrollment-sets/DEVICE-UDID-1234", reqs[4].Path)
	assert.Contains(t, reqs[4].Query, "set=DEVICE-UDID-1234")
	// noNotify should be false (absent from query or set to false)
	assert.NotContains(t, reqs[4].Query, "nonotify=true")
}

func TestPushProfileViaDDM_UnchangedDeclarations_TouchCalled(t *testing.T) {
	server, requests, statusOverrides := newMockKMFDDM(t)
	defer server.Close()

	// Override PUT declarations to return 204 (unchanged)
	statusOverrides["PUT /v1/declarations"] = http.StatusNoContent

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")

	err := PushProfileViaDDM(client, "DEVICE-UDID-1234", "com.example.wifi", "https://mdm.example.com")
	require.NoError(t, err)

	// When declarations are unchanged (204), touch calls should be made.
	// Expected: PUT decl (legacy), POST touch (legacy), PUT decl (activation),
	// POST touch (activation), PUT set-decl (legacy), PUT set-decl (activation),
	// PUT enrollment-set
	assert.Len(t, *requests, 7)

	reqs := *requests

	// Step 1: PUT LegacyProfile declaration (returns 204 = unchanged)
	assert.Equal(t, "PUT", reqs[0].Method)
	assert.Equal(t, "/v1/declarations", reqs[0].Path)

	// Step 1b: POST touch for LegacyProfile
	assert.Equal(t, "POST", reqs[1].Method)
	assert.Equal(t, "/v1/declarations/biz.airbnb.DEVICE-UDID-1234.legacy_profile.com.example.wifi/touch", reqs[1].Path)
	assert.Contains(t, reqs[1].Query, "nonotify=true")

	// Step 2: PUT ActivationSimple declaration (returns 204 = unchanged)
	assert.Equal(t, "PUT", reqs[2].Method)
	assert.Equal(t, "/v1/declarations", reqs[2].Path)

	// Step 2b: POST touch for ActivationSimple
	assert.Equal(t, "POST", reqs[3].Method)
	assert.Equal(t, "/v1/declarations/biz.airbnb.DEVICE-UDID-1234.legacy_profile_activation.com.example.wifi/touch", reqs[3].Path)
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

func TestPushProfileViaDDM_ActivationReferencesLegacyDeclaration(t *testing.T) {
	server, requests, _ := newMockKMFDDM(t)
	defer server.Close()

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")

	err := PushProfileViaDDM(client, "DEVICE-UDID-1234", "com.example.wifi", "https://mdm.example.com")
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

	expectedLegacyID := "biz.airbnb.DEVICE-UDID-1234.legacy_profile.com.example.wifi"
	assert.Len(t, activationDecl.Payload.StandardConfigurations, 1)
	assert.Equal(t, expectedLegacyID, activationDecl.Payload.StandardConfigurations[0])
}

func TestPushProfileViaDDM_PutDeclarationError(t *testing.T) {
	server, _, statusOverrides := newMockKMFDDM(t)
	defer server.Close()

	statusOverrides["PUT /v1/declarations"] = http.StatusInternalServerError

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")

	err := PushProfileViaDDM(client, "DEVICE-UDID-1234", "com.example.wifi", "https://mdm.example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PUT LegacyProfile declaration")
}

func TestPushProfileViaDDM_TouchError(t *testing.T) {
	server, _, statusOverrides := newMockKMFDDM(t)
	defer server.Close()

	// Declarations return 204 (unchanged) so touch is called
	statusOverrides["PUT /v1/declarations"] = http.StatusNoContent
	// Touch returns 500
	statusOverrides["POST /v1/declarations/biz.airbnb.DEVICE-UDID-1234.legacy_profile.com.example.wifi/touch"] = http.StatusInternalServerError

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")

	err := PushProfileViaDDM(client, "DEVICE-UDID-1234", "com.example.wifi", "https://mdm.example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "touch LegacyProfile declaration")
}

func TestPushProfileViaDDM_SpecialCharactersInPayloadIdentifier(t *testing.T) {
	server, requests, _ := newMockKMFDDM(t)
	defer server.Close()

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")

	err := PushProfileViaDDM(client, "DEVICE-UDID-1234", "com.example.profile with spaces!", "https://mdm.example.com")
	require.NoError(t, err)

	reqs := *requests
	// The declaration ID should use the sanitized profile ID
	var legacyDecl ddm.Declaration
	err = json.Unmarshal([]byte(reqs[0].Body), &legacyDecl)
	require.NoError(t, err)
	// Spaces and ! replaced with underscores
	assert.Equal(t, "biz.airbnb.DEVICE-UDID-1234.legacy_profile.com.example.profile_with_spaces_", legacyDecl.Identifier)

	// But the ProfileURL should use the original payload identifier
	var payload struct {
		Payload struct {
			ProfileURL string `json:"ProfileURL"`
		} `json:"Payload"`
	}
	err = json.Unmarshal([]byte(reqs[0].Body), &payload)
	require.NoError(t, err)
	assert.Contains(t, payload.Payload.ProfileURL, "com.example.profile with spaces!")
}

func TestDeleteProfileViaDDM_Success(t *testing.T) {
	server, requests, _ := newMockKMFDDM(t)
	defer server.Close()

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")

	err := DeleteProfileViaDDM(client, "DEVICE-UDID-1234", "com.example.wifi")
	require.NoError(t, err)

	assert.Len(t, *requests, 5)

	reqs := *requests

	// Step 1: DELETE set-declaration for LegacyProfile
	assert.Equal(t, "DELETE", reqs[0].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[0].Path)
	assert.Contains(t, reqs[0].Query, "declaration=biz.airbnb.DEVICE-UDID-1234.legacy_profile.com.example.wifi")
	assert.Contains(t, reqs[0].Query, "nonotify=true")

	// Step 2: DELETE set-declaration for ActivationSimple
	assert.Equal(t, "DELETE", reqs[1].Method)
	assert.Equal(t, "/v1/set-declarations/DEVICE-UDID-1234", reqs[1].Path)
	assert.Contains(t, reqs[1].Query, "declaration=biz.airbnb.DEVICE-UDID-1234.legacy_profile_activation.com.example.wifi")
	assert.Contains(t, reqs[1].Query, "nonotify=true")

	// Step 3: DELETE declaration (legacy)
	assert.Equal(t, "DELETE", reqs[2].Method)
	assert.Equal(t, "/v1/declarations/biz.airbnb.DEVICE-UDID-1234.legacy_profile.com.example.wifi", reqs[2].Path)
	assert.Contains(t, reqs[2].Query, "nonotify=true")

	// Step 4: DELETE declaration (activation)
	assert.Equal(t, "DELETE", reqs[3].Method)
	assert.Equal(t, "/v1/declarations/biz.airbnb.DEVICE-UDID-1234.legacy_profile_activation.com.example.wifi", reqs[3].Path)
	assert.Contains(t, reqs[3].Query, "nonotify=true")

	// Step 5: PUT enrollment-set (noNotify=false to trigger sync)
	assert.Equal(t, "PUT", reqs[4].Method)
	assert.Equal(t, "/v1/enrollment-sets/DEVICE-UDID-1234", reqs[4].Path)
	assert.Contains(t, reqs[4].Query, "set=DEVICE-UDID-1234")
	assert.NotContains(t, reqs[4].Query, "nonotify=true")
}

func TestDeleteProfileViaDDM_DeleteSetDeclarationError(t *testing.T) {
	server, _, statusOverrides := newMockKMFDDM(t)
	defer server.Close()

	statusOverrides["DELETE /v1/set-declarations/DEVICE-UDID-1234"] = http.StatusInternalServerError

	client := ddm.NewKMFDDMClient(server.URL, "testapikey")

	err := DeleteProfileViaDDM(client, "DEVICE-UDID-1234", "com.example.wifi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DELETE set-declaration (legacy)")
}
