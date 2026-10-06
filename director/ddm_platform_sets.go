package director

// Platform set binding
//
// KMFDDM delivers declarations only through sets an enrollment is bound to, and has no
// "all devices" set. When ddm-platform-sets is configured (e.g.
// "macos=com.example.ddm.macos,ios=com.example.ddm.ios"), each device is bound to the
// set for its platform: at enrollment (RunInitialTasks), and for existing devices by the
// fleet and per-device DDM activation paths. The set's contents are managed elsewhere;
// this only binds. Binding is idempotent, so repeating it is harmless.

import (
	"fmt"
	"strings"

	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
)

// Platforms a device can map to, keyed in ddm-platform-sets.
const (
	PlatformMacOS    = "macos"
	PlatformIOS      = "ios"
	PlatformIPadOS   = "ipados"
	PlatformTVOS     = "tvos"
	PlatformWatchOS  = "watchos"
	PlatformVisionOS = "visionos"
)

var knownPlatforms = map[string]bool{
	PlatformMacOS: true, PlatformIOS: true, PlatformIPadOS: true,
	PlatformTVOS: true, PlatformWatchOS: true, PlatformVisionOS: true,
}

// ParsePlatformSets parses "platform=set,platform=set". Two platforms may share a set
// (e.g. ios and ipados). An empty string means binding is off.
func ParsePlatformSets(s string) (map[string]string, error) {
	sets := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		platform, set, ok := strings.Cut(pair, "=")
		platform, set = strings.TrimSpace(platform), strings.TrimSpace(set)
		if !ok || set == "" {
			return nil, fmt.Errorf("ddm-platform-sets: %q is not platform=set", pair)
		}
		if !knownPlatforms[platform] {
			return nil, fmt.Errorf("ddm-platform-sets: unknown platform %q (want macos, ios, ipados, tvos, watchos or visionos)", platform)
		}
		if _, dup := sets[platform]; dup {
			return nil, fmt.Errorf("ddm-platform-sets: platform %q listed twice", platform)
		}
		sets[platform] = set
	}
	return sets, nil
}

// devicePlatform derives the platform from the hardware ProductName reported at
// Authenticate (e.g. "Mac14,2", "MacBookPro18,3", "iPhone15,3", "iPad13,1"), falling
// back to Model. It returns "" when neither is recognised.
func devicePlatform(device types.Device) string {
	for _, name := range []string{device.ProductName, device.Model} {
		switch {
		case name == "":
			continue
		case strings.HasPrefix(name, "iPhone"), strings.HasPrefix(name, "iPod"):
			return PlatformIOS
		case strings.HasPrefix(name, "iPad"):
			return PlatformIPadOS
		case strings.HasPrefix(name, "AppleTV"):
			return PlatformTVOS
		case strings.HasPrefix(name, "Watch"):
			return PlatformWatchOS
		case strings.HasPrefix(name, "RealityDevice"):
			return PlatformVisionOS
		case strings.HasPrefix(name, "Mac"), strings.HasPrefix(name, "iMac"),
			strings.HasPrefix(name, "VirtualMac"), strings.HasPrefix(name, "Xserve"):
			return PlatformMacOS
		}
	}
	return ""
}

// platformSetFor returns the set to bind device to, or "" when binding is off, the
// platform is unknown, or the platform has no set.
func platformSetFor(device types.Device) (string, error) {
	sets, err := ParsePlatformSets(utils.DDMPlatformSets())
	if err != nil || len(sets) == 0 {
		return "", err
	}
	return sets[devicePlatform(device)], nil
}

// bindPlatformSet binds device to its platform set. It is a no-op when there is none.
// noNotify suppresses KMFDDM's DeclarativeManagement push; pass false so a newly bound
// device syncs, or true when the caller notifies right after.
func bindPlatformSet(client *ddm.KMFDDMClient, device types.Device, noNotify bool) error {
	set, err := platformSetFor(device)
	if err != nil || set == "" {
		return err
	}
	if err := client.PutEnrollmentSet(device.UDID, set, noNotify); err != nil {
		return fmt.Errorf("bindPlatformSet: %s to %s: %w", device.UDID, set, err)
	}
	DebugLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "bound to DDM platform set " + set})
	return nil
}

// bindPlatformSetAtEnrollment binds a newly enrolled device, then notifies it. The
// notify is unconditional: on a re-enrollment (an erased device keeps its UDID) the
// binding already exists, so KMFDDM returns 304 and pushes nothing, yet the device's
// declarative state is gone and it must sync again. A failure is logged and does not
// fail initial tasks; the next fleet activation retries it.
func bindPlatformSetAtEnrollment(device types.Device) {
	set, err := platformSetFor(device)
	if err != nil || set == "" {
		if err != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
		}
		return
	}
	client, err := ddm.Client()
	if err != nil {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "bindPlatformSetAtEnrollment: " + err.Error()})
		return
	}
	if err := bindPlatformSet(client, device, true); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
		return
	}
	err = client.NotifyEnrollment(device.UDID)
	observeDDMNotify(err)
	if err != nil {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "bindPlatformSetAtEnrollment: notify: " + err.Error()})
	}
}
