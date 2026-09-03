package director

import (
	"strconv"
	"strings"

	"github.com/mdmdirector/mdmdirector/types"
)

// architecture is what a device's hardware model proves about its CPU
type architecture string

const (
	archSilicon architecture = "silicon"
	archIntel   architecture = "intel"
	archUnknown architecture = "unknown"
)

// siliconThresholds is the first Apple Silicon generation of each legacy model family
var siliconThresholds = map[string]int{
	"MacBookAir": 10,
	"MacBookPro": 17,
	"Macmini":    9,
	"iMac":       21,
	"MacPro":     8, // the last Intel Mac Pro is 7,1
}

// classifyModel reads an Apple model identifier as reported in DeviceInformation
// ("MacBookPro16,1", "Mac14,2") and reports what it proves
func classifyModel(model string) architecture {
	model = strings.TrimSpace(model)
	if model == "" {
		return archUnknown
	}

	// The unified "Mac<gen>,<n>" family (Mac13,1 / Mac14,2 / Mac16,5 ...) is Silicon-only, as
	// are Apple Virtualization guests, which only run on Apple Silicon hosts.
	if strings.HasPrefix(model, "VirtualMac") {
		return archSilicon
	}
	if strings.HasPrefix(model, "Mac") && len(model) >= 4 && model[3] >= '1' && model[3] <= '9' {
		return archSilicon
	}

	family, generation, ok := splitModel(model)
	if !ok {
		return archUnknown
	}

	// The iMac Pro was Intel-only and was never succeeded.
	if family == "iMacPro" {
		return archIntel
	}

	threshold, known := siliconThresholds[family]
	if !known {
		return archUnknown
	}
	if generation >= threshold {
		return archSilicon
	}
	return archIntel
}

// splitModel breaks "MacBookPro16,1" into ("MacBookPro", 16). ok is false when the string is
// not shaped like a model identifier
func splitModel(model string) (family string, generation int, ok bool) {
	base, _, hasComma := strings.Cut(model, ",")
	if !hasComma {
		return "", 0, false
	}

	digits := strings.LastIndexFunc(base, func(r rune) bool { return r < '0' || r > '9' }) + 1
	if digits == 0 || digits == len(base) {
		return "", 0, false
	}

	generation, err := strconv.Atoi(base[digits:])
	if err != nil {
		return "", 0, false
	}
	return base[:digits], generation, true
}

// deviceArchitecture classifies a device from the model DeviceInformation reported
func deviceArchitecture(device types.Device) architecture {
	return classifyModel(device.Model)
}

// canReEnrollViaACME reports whether it is safe to push a fresh enrollment profile to this
// device. Only a positive identification of Apple Silicon qualifies: an unknown model is
// treated like Intel, which for a Silicon Mac merely defers its ACME renewal until the next
// DeviceInformation lands, and for an Intel Mac is the only correct answer.
func canReEnrollViaACME(device types.Device) bool {
	return deviceArchitecture(device) == archSilicon
}
