package ddm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPutDeclaration_Changed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/v1/declarations", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("nonotify"))

		// Verify basic auth
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "kmfddm", user)
		assert.Equal(t, "test-api-key", pass)

		// Verify body contains declaration
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var decl Declaration
		err = json.Unmarshal(body, &decl)
		require.NoError(t, err)
		assert.Equal(t, "test.declaration.id", decl.Identifier)
		assert.Equal(t, TypeLegacyProfile, decl.Type)

		// 304 = changed/new in KMFDDM
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	decl := Declaration{
		Identifier: "test.declaration.id",
		Type:       TypeLegacyProfile,
		Payload: LegacyProfilePayload{
			ProfileURL: "https://example.com/profiledownload/udid/profile",
		},
	}

	changed, err := kmfddmClient.PutDeclaration(decl, true)
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestPutDeclaration_Unchanged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 204 = unchanged in KMFDDM
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	decl := Declaration{
		Identifier: "test.declaration.id",
		Type:       TypeLegacyProfile,
		Payload:    LegacyProfilePayload{ProfileURL: "https://example.com/profile"},
	}

	changed, err := kmfddmClient.PutDeclaration(decl, true)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestPutDeclaration_NoNotifyFalse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// nonotify should not be in query params
		assert.Empty(t, r.URL.Query().Get("nonotify"))
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	decl := Declaration{
		Identifier: "test.id",
		Type:       TypeActivationSimple,
		Payload:    ActivationSimplePayload{StandardConfigurations: []string{"test.config"}},
	}

	changed, err := kmfddmClient.PutDeclaration(decl, false)
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestPutDeclaration_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal error"}`))
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	decl := Declaration{Identifier: "test.id", Type: TypeLegacyProfile, Payload: LegacyProfilePayload{}}

	_, err := kmfddmClient.PutDeclaration(decl, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestTouchDeclaration_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/declarations/biz.airbnb.udid123.legacy_profile.com.test/touch", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("nonotify"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	err := kmfddmClient.TouchDeclaration("biz.airbnb.udid123.legacy_profile.com.test", true)
	require.NoError(t, err)
}

func TestTouchDeclaration_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	err := kmfddmClient.TouchDeclaration("nonexistent.declaration", true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPutSetDeclaration_Changed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/v1/set-declarations/device-udid-123", r.URL.Path)
		assert.Equal(t, "biz.airbnb.device-udid-123.legacy_profile.com.test", r.URL.Query().Get("declaration"))
		assert.Equal(t, "true", r.URL.Query().Get("nonotify"))
		// 204 = changed for set-declarations
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	err := kmfddmClient.PutSetDeclaration("device-udid-123", "biz.airbnb.device-udid-123.legacy_profile.com.test", true)
	require.NoError(t, err)
}

func TestPutSetDeclaration_Unchanged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 304 = unchanged for set-declarations
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	err := kmfddmClient.PutSetDeclaration("device-udid-123", "some.declaration", true)
	require.NoError(t, err)
}

func TestPutSetDeclaration_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"fail"}`))
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	err := kmfddmClient.PutSetDeclaration("udid", "decl", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestPutEnrollmentSet_Changed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/v1/enrollment-sets/device-udid-123", r.URL.Path)
		assert.Equal(t, "device-udid-123", r.URL.Query().Get("set"))
		assert.Empty(t, r.URL.Query().Get("nonotify"))
		// 204 = changed
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	err := kmfddmClient.PutEnrollmentSet("device-udid-123", "device-udid-123", false)
	require.NoError(t, err)
}

func TestPutEnrollmentSet_Unchanged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "true", r.URL.Query().Get("nonotify"))
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	err := kmfddmClient.PutEnrollmentSet("device-udid-123", "device-udid-123", true)
	require.NoError(t, err)
}

func TestDeleteDeclaration_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/v1/declarations/biz.airbnb.udid123.legacy_profile.com.test", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("nonotify"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	err := kmfddmClient.DeleteDeclaration("biz.airbnb.udid123.legacy_profile.com.test", true)
	require.NoError(t, err)
}

func TestDeleteDeclaration_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	err := kmfddmClient.DeleteDeclaration("nonexistent.declaration", true)
	// Not found is not an error for deletion — declaration already gone
	require.NoError(t, err)
}

func TestDeleteSetDeclaration_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/v1/set-declarations/device-udid-123", r.URL.Path)
		assert.Equal(t, "biz.airbnb.device-udid-123.legacy_profile.com.test", r.URL.Query().Get("declaration"))
		assert.Equal(t, "true", r.URL.Query().Get("nonotify"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	err := kmfddmClient.DeleteSetDeclaration("device-udid-123", "biz.airbnb.device-udid-123.legacy_profile.com.test", true)
	require.NoError(t, err)
}

func TestDeleteSetDeclaration_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	kmfddmClient := NewKMFDDMClient(server.URL, "test-api-key")
	err := kmfddmClient.DeleteSetDeclaration("device-udid-123", "nonexistent.declaration", true)
	// Not found is not an error for deletion — association already gone
	require.NoError(t, err)
}
