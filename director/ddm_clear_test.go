package director

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	prometheustestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The enrollment-set association must be the first thing to go, so a device that syncs
// mid-teardown sees an empty declaration-items rather than a partial set. Every
// declaration is then removed from the set, but only the ones mdmdirector created
// (<declaration prefix>.<udid>.…) are deleted; a declaration another system put in the
// set is detached and left in KMFDDM. Nothing notifies.
func TestClearDDMForDevice_DropsAssociationFirstThenEveryDeclaration(t *testing.T) {
	setDDMPrefixes(t)
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
			_, _ = w.Write([]byte(`["p.udid-1.legacy_profile.a","p.udid-1.legacy_profile_activation.a","p.udid-1.package.x","com.other.system.thing"]`))
		case r.Method == "DELETE" && r.URL.Path == "/v1/set-declarations/pfx.udid-1":
			assert.Equal(t, "true", r.URL.Query().Get("nonotify"))
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "DELETE" && len(r.URL.Path) > len("/v1/declarations/") && r.URL.Path[:len("/v1/declarations/")] == "/v1/declarations/":
			assert.Equal(t, "true", r.URL.Query().Get("nonotify"))
			assert.True(t, strings.HasPrefix(r.URL.Path, "/v1/declarations/p.udid-1."), "only mdmdirector's own declarations may be deleted, got %s", r.URL.Path)
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
	// 3 own declarations: detach + delete each; 1 foreign: detach only
	assert.Len(t, calls, 2+3*2+1)
	assert.Contains(t, calls, "DELETE /v1/set-declarations/pfx.udid-1?declaration=com.other.system.thing&nonotify=true")
}

func TestClearDDMForDevice_EmptySet(t *testing.T) {
	setDDMPrefixes(t)
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
	setDDMPrefixes(t)
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "DELETE" && r.URL.Path == "/v1/enrollment-sets/udid-1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "GET" && r.URL.Path == "/v1/set-declarations/pfx.udid-1":
			_, _ = w.Write([]byte(`["p.udid-1.legacy_profile.bad","p.udid-1.legacy_profile.good"]`))
		case r.Method == "DELETE" && r.URL.Path == "/v1/set-declarations/pfx.udid-1":
			if r.URL.Query().Get("declaration") == "p.udid-1.legacy_profile.bad" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "DELETE" && r.URL.Path == "/v1/declarations/p.udid-1.legacy_profile.good":
			deleted = append(deleted, "good")
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	removed, err := clearDDMForDevice(ddm.NewKMFDDMClient(server.URL, "key"), "udid-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "legacy_profile.bad")
	assert.Equal(t, 1, removed)
	assert.Equal(t, []string{"good"}, deleted)
}

// If the association delete fails nothing else is attempted: deleting declarations while
// the enrollment still points at the set is exactly the partial-sync window to avoid.
func TestClearDDMForDevice_StopsIfAssociationDeleteFails(t *testing.T) {
	setDDMPrefixes(t)
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

// setDDMPrefixes points ddm-set-prefix at "pfx" and ddm-declaration-prefix at "p" for the
// test, so the set the cleanup must use is "pfx.<udid>" and the declarations it may delete
// start with "p.<udid>.".
func setDDMPrefixes(t *testing.T) {
	t.Helper()
	for name, value := range map[string]string{"ddm-set-prefix": "pfx", "ddm-declaration-prefix": "p"} {
		if flag.Lookup(name) == nil {
			flag.String(name, "", name)
		}
		old := flag.Lookup(name).Value.String()
		require.NoError(t, flag.Set(name, value))
		t.Cleanup(func() { _ = flag.Set(name, old) })
	}
}

// resetDDMForEnrollment's gate: with clear-device-on-enroll on the cleanup always runs;
// with it off it runs only for a device that is not going to use DDM, since a device that
// stays on DDM has its declarations re-PUT by the push and a cleanup would only churn.
func TestResetDDMForEnrollment_Gate(t *testing.T) {
	cases := []struct {
		name        string
		clear       bool
		globalDDM   bool
		optedIn     bool // only consulted when globalDDM is off
		wantCleanup bool
	}{
		{"clear on, global DDM on", true, true, false, true},
		{"clear on, global DDM off, opted in", true, false, true, true},
		{"clear off, global DDM on", false, true, false, false},
		{"clear off, global DDM off, opted in", false, false, true, false},
		{"clear off, global DDM off, not opted in", false, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setDDMPrefixes(t)
			setupDDMFlags(t, tc.globalDDM, tc.globalDDM)
			oldProvider := utils.FlagProvider
			utils.FlagProvider = mockFlagBuilder{tc.clear}
			t.Cleanup(func() { utils.FlagProvider = oldProvider })

			mockSpy, cleanup := setupMockDB(t)
			defer cleanup()
			if !tc.globalDDM {
				count := int64(0)
				if tc.optedIn {
					count = 1
				}
				// ddmForDevice; a 0 falls through to ddmPackagesForDevice, which asks again
				mockSpy.ExpectQuery(`SELECT count\(\*\) FROM "ddm_opt_ins"`).WithArgs("udid-1").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
				if !tc.optedIn {
					mockSpy.ExpectQuery(`SELECT count\(\*\) FROM "ddm_opt_ins"`).WithArgs("udid-1").
						WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
				}
			}

			var enrollmentSetDeletes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "DELETE" && r.URL.Path == "/v1/enrollment-sets/udid-1":
					enrollmentSetDeletes.Add(1)
					w.WriteHeader(http.StatusNoContent)
				case r.Method == "GET" && r.URL.Path == "/v1/set-declarations/pfx.udid-1":
					w.WriteHeader(http.StatusNotFound)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()
			ddm.InitClient(server.URL, "key")

			err := resetDDMForEnrollment(types.Device{UDID: "udid-1", SerialNumber: "S"})

			require.NoError(t, err)
			if tc.wantCleanup {
				assert.Equal(t, int32(1), enrollmentSetDeletes.Load(), "cleanup should have run")
			} else {
				assert.Equal(t, int32(0), enrollmentSetDeletes.Load(), "cleanup should have been skipped")
			}
			assert.NoError(t, mockSpy.ExpectationsWereMet())
		})
	}
}

// initialTasksPending is the gate every non-RunInitialTasks push path checks; a deferral
// is counted so a device stuck before its initial tasks is visible
func TestInitialTasksPending(t *testing.T) {
	if flag.Lookup("prometheus") == nil {
		flag.Bool("prometheus", false, "Enable prometheus metrics")
	}
	require.NoError(t, flag.Set("prometheus", "true"))
	t.Cleanup(func() { _ = flag.Set("prometheus", "false") })

	counter := metrics.PushesDeferred(deferPathIdleInfoRequest)
	before := prometheustestutil.ToFloat64(counter)

	assert.False(t, initialTasksPending(types.Device{UDID: "u", InitialTasksRun: true}, deferPathIdleInfoRequest))
	assert.Equal(t, before, prometheustestutil.ToFloat64(counter))

	assert.True(t, initialTasksPending(types.Device{UDID: "u", InitialTasksRun: false}, deferPathIdleInfoRequest))
	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))
}
