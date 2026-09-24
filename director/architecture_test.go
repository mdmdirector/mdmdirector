package director

import (
	"testing"

	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
)

func TestClassifyModel(t *testing.T) {
	cases := map[string]architecture{
		// Unified Silicon family and VMs
		"Mac13,1":       archSilicon,
		"Mac14,2":       archSilicon,
		"Mac16,5":       archSilicon,
		"VirtualMac2,1": archSilicon,

		// Legacy families at and past their Silicon threshold
		"MacBookAir10,1": archSilicon,
		"MacBookPro17,1": archSilicon,
		"MacBookPro18,3": archSilicon,
		"Macmini9,1":     archSilicon,
		"iMac21,1":       archSilicon,

		// Legacy families before their threshold - Intel
		"MacBookAir9,1":  archIntel,
		"MacBookPro16,1": archIntel,
		"MacBookPro15,2": archIntel,
		"Macmini8,1":     archIntel,
		"iMac20,1":       archIntel,
		"iMac19,1":       archIntel,
		"MacPro7,1":      archIntel,
		"iMacPro1,1":     archIntel,

		// Not readable - must never be mistaken for Silicon
		"":                archUnknown,
		"   ":             archUnknown,
		"MacBookPro":      archUnknown,
		"MacBook Pro":     archUnknown,
		"iPad13,1":        archUnknown,
		"Macmini":         archUnknown,
		"UnknownThing1,1": archUnknown,
		"MacBookPro16":    archUnknown,
	}

	for model, want := range cases {
		assert.Equalf(t, want, classifyModel(model), "model %q", model)
	}
}

func TestCanReEnrollViaACME(t *testing.T) {
	assert.True(t, canReEnrollViaACME(types.Device{Model: "MacBookPro18,3"}))
	assert.True(t, canReEnrollViaACME(types.Device{Model: "Mac14,2"}))

	assert.False(t, canReEnrollViaACME(types.Device{Model: "MacBookPro16,1"}), "Intel must never re-enroll")
	assert.False(t, canReEnrollViaACME(types.Device{}), "unknown model must fail safe")
}
