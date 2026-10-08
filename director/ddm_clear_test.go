package director

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The enrollment-set association must be the first thing to go, so a device that syncs
// mid-teardown sees an empty declaration-items rather than a partial set; every
// declaration in the set is then removed from the set and deleted, and nothing notifies.
func TestClearDDMForDevice_DropsAssociationFirstThenEveryDeclaration(t *testing.T) {
	setDDMSetPrefix(t, "pfx")
	var mu sync.Mutex
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		switch {
		case r.Method == "DELETE" && r.URL.Path == "/v1/enrollment-sets/udid-1":
			assert.Equal(t, "true", r.URL.Query().Get("nonotify"))
			assert.Equal(t, "pfx.udid-1", r.URL.Query().Get("set"))
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "GET" && r.URL.Path == "/v1/set-declarations/pfx.udid-1":
			_, _ = w.Write([]byte(`["p.udid-1.legacy_profile.a","p.udid-1.legacy_profile_activation.a","p.udid-1.package.x"]`))
		case r.Method == "DELETE" && r.URL.Path == "/v1/set-declarations/pfx.udid-1":
			assert.Equal(t, "true", r.URL.Query().Get("nonotify"))
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "DELETE" && len(r.URL.Path) > len("/v1/declarations/") && r.URL.Path[:len("/v1/declarations/")] == "/v1/declarations/":
			assert.Equal(t, "true", r.URL.Query().Get("nonotify"))
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/v1/notify":
			t.Errorf("clearDDMForDevice must not notify the device")
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	defer server.Close()

	removed, err := clearDDMForDevice(ddm.NewKMFDDMClient(server.URL, "key"), "udid-1")
	require.NoError(t, err)
	assert.Equal(t, 3, removed)

	require.NotEmpty(t, calls)
	assert.Equal(t, "DELETE /v1/enrollment-sets/udid-1?nonotify=true&set=pfx.udid-1", calls[0])
	assert.Equal(t, "GET /v1/set-declarations/pfx.udid-1?", calls[1])
	assert.Len(t, calls, 2+3*2)
}

func TestClearDDMForDevice_EmptySet(t *testing.T) {
	setDDMSetPrefix(t, "pfx")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "DELETE" && r.URL.Path == "/v1/enrollment-sets/udid-1":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == "GET" && r.URL.Path == "/v1/set-declarations/pfx.udid-1":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	removed, err := clearDDMForDevice(ddm.NewKMFDDMClient(server.URL, "key"), "udid-1")
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

// A failed delete is reported, but the remaining declarations are still attempted.
func TestClearDDMForDevice_ContinuesPastFailures(t *testing.T) {
	setDDMSetPrefix(t, "pfx")
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "DELETE" && r.URL.Path == "/v1/enrollment-sets/udid-1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "GET" && r.URL.Path == "/v1/set-declarations/pfx.udid-1":
			_, _ = w.Write([]byte(`["bad","good"]`))
		case r.Method == "DELETE" && r.URL.Path == "/v1/set-declarations/pfx.udid-1":
			if r.URL.Query().Get("declaration") == "bad" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "DELETE" && r.URL.Path == "/v1/declarations/good":
			deleted = append(deleted, "good")
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	removed, err := clearDDMForDevice(ddm.NewKMFDDMClient(server.URL, "key"), "udid-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad")
	assert.Equal(t, 1, removed)
	assert.Equal(t, []string{"good"}, deleted)
}

// If the association delete fails nothing else is attempted: deleting declarations while
// the enrollment still points at the set is exactly the partial-sync window to avoid.
func TestClearDDMForDevice_StopsIfAssociationDeleteFails(t *testing.T) {
	setDDMSetPrefix(t, "pfx")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" && r.URL.Path == "/v1/enrollment-sets/udid-1" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	_, err := clearDDMForDevice(ddm.NewKMFDDMClient(server.URL, "key"), "udid-1")
	require.Error(t, err)
}

func setDDMSetPrefix(t *testing.T, prefix string) {
	t.Helper()
	if flag.Lookup("ddm-set-prefix") == nil {
		flag.String("ddm-set-prefix", "", "ddm-set-prefix")
	}
	old := flag.Lookup("ddm-set-prefix").Value.String()
	require.NoError(t, flag.Set("ddm-set-prefix", prefix))
	t.Cleanup(func() { _ = flag.Set("ddm-set-prefix", old) })
}
