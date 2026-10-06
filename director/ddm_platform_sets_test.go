package director

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setPlatformSets registers ddm-platform-sets if needed and sets it for the test
func setPlatformSets(t *testing.T, value string) {
	t.Helper()
	if flag.Lookup("ddm-platform-sets") == nil {
		flag.String("ddm-platform-sets", "", "")
	}
	prev := flag.Lookup("ddm-platform-sets").Value.String()
	require.NoError(t, flag.Set("ddm-platform-sets", value))
	t.Cleanup(func() { _ = flag.Set("ddm-platform-sets", prev) })
}

// recordingKMFDDM records every enrollment-set binding as "udid set nonotify", and
// every notify by enrollment ID
type recordingKMFDDM struct {
	mu       sync.Mutex
	bindings []string
	notified []string
	// bindStatus is returned for enrollment-set PUTs: 204 for a new binding, 304 for
	// one that already exists (a re-enrollment)
	bindStatus int
}

func (k *recordingKMFDDM) Bindings() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := append([]string(nil), k.bindings...)
	sort.Strings(out)
	return out
}

func (k *recordingKMFDDM) Notified() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.notified...)
}

func startRecordingKMFDDM(t *testing.T) *recordingKMFDDM {
	t.Helper()
	if flag.Lookup("prometheus") == nil {
		flag.Bool("prometheus", false, "Enable prometheus metrics")
	}
	k := &recordingKMFDDM{bindStatus: http.StatusNoContent}
	handler := http.NewServeMux()
	handler.HandleFunc("/v1/enrollment-sets/", func(w http.ResponseWriter, r *http.Request) {
		k.mu.Lock()
		udid := r.URL.Path[len("/v1/enrollment-sets/"):]
		k.bindings = append(k.bindings, udid+" "+r.URL.Query().Get("set")+" "+r.URL.Query().Get("nonotify"))
		status := k.bindStatus
		k.mu.Unlock()
		w.WriteHeader(status)
	})
	handler.HandleFunc("/v1/notify", func(w http.ResponseWriter, r *http.Request) {
		k.mu.Lock()
		k.notified = append(k.notified, r.URL.Query().Get("id"))
		k.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	ddm.InitClient(server.URL, "test-key")
	return k
}

func TestParsePlatformSets(t *testing.T) {
	sets, err := ParsePlatformSets(" macos=set.mac, ios=set.mobile,ipados=set.mobile ,")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"macos": "set.mac", "ios": "set.mobile", "ipados": "set.mobile"}, sets)

	sets, err = ParsePlatformSets("")
	require.NoError(t, err)
	assert.Empty(t, sets)

	for _, bad := range []string{"macos", "macos=", "android=set", "macos=a,macos=b"} {
		_, err := ParsePlatformSets(bad)
		assert.Error(t, err, bad)
	}
}

func TestDevicePlatform(t *testing.T) {
	tests := map[string]types.Device{
		PlatformMacOS:    {ProductName: "Mac14,2"},
		PlatformIOS:      {ProductName: "iPhone15,3"},
		PlatformIPadOS:   {ProductName: "iPad13,1"},
		PlatformTVOS:     {ProductName: "AppleTV11,1"},
		PlatformWatchOS:  {ProductName: "Watch6,1"},
		PlatformVisionOS: {ProductName: "RealityDevice14,1"},
		"":               {},
	}
	for want, device := range tests {
		assert.Equal(t, want, devicePlatform(device), device.ProductName)
	}
	// Intel and virtual Macs, and the Model fallback
	for _, d := range []types.Device{{ProductName: "MacBookPro16,1"}, {ProductName: "iMac20,1"}, {ProductName: "Macmini9,1"}, {ProductName: "VirtualMac2,1"}, {Model: "MacBookAir10,1"}} {
		assert.Equal(t, PlatformMacOS, devicePlatform(d), d.ProductName+d.Model)
	}
}

// Fleet activation binds each device to its platform set (no push; activateDDM
// notifies), alongside the per-device set. A platform with no set is not bound.
func TestActivateDevices_BindsPlatformSets(t *testing.T) {
	setPlatformSets(t, "macos=set.mac,ios=set.mobile,ipados=set.mobile")
	kmfddm := startRecordingKMFDDM(t)
	client, err := ddm.Client()
	require.NoError(t, err)

	devices := []types.Device{
		{UDID: "mac", ProductName: "Mac14,2"},
		{UDID: "phone", ProductName: "iPhone15,3"},
		{UDID: "pad", ProductName: "iPad13,1"},
		{UDID: "tv", ProductName: "AppleTV11,1"},
	}
	activated, err := activateDevices(client, devices)
	require.NoError(t, err)
	assert.Equal(t, 4, activated)
	assert.Equal(t, []string{
		"mac mac true", "mac set.mac true",
		"pad pad true", "pad set.mobile true",
		"phone phone true", "phone set.mobile true",
		"tv tv true",
	}, kmfddm.Bindings())
}

// With the flag unset nothing extra is bound
func TestActivateDevices_NoPlatformSets(t *testing.T) {
	setPlatformSets(t, "")
	kmfddm := startRecordingKMFDDM(t)
	client, err := ddm.Client()
	require.NoError(t, err)

	_, err = activateDevices(client, []types.Device{{UDID: "mac", ProductName: "Mac14,2"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"mac mac true"}, kmfddm.Bindings())
}

// At enrollment the device is bound, then notified once. A device with no platform
// set gets neither.
func TestBindPlatformSetAtEnrollment_BindsAndNotifies(t *testing.T) {
	setPlatformSets(t, "macos=set.mac")
	kmfddm := startRecordingKMFDDM(t)

	bindPlatformSetAtEnrollment(types.Device{UDID: "mac", ProductName: "Mac14,2"})
	bindPlatformSetAtEnrollment(types.Device{UDID: "phone", ProductName: "iPhone15,3"})
	assert.Equal(t, []string{"mac set.mac true"}, kmfddm.Bindings())
	assert.Equal(t, []string{"mac"}, kmfddm.Notified())
}

// On re-enrollment the binding already exists (KMFDDM returns 304 and would push
// nothing), but the erased device must still be told to sync
func TestBindPlatformSetAtEnrollment_ReenrollmentStillNotifies(t *testing.T) {
	setPlatformSets(t, "macos=set.mac")
	kmfddm := startRecordingKMFDDM(t)
	kmfddm.bindStatus = http.StatusNotModified

	bindPlatformSetAtEnrollment(types.Device{UDID: "mac", ProductName: "Mac14,2"})
	assert.Equal(t, []string{"mac"}, kmfddm.Notified())
}
