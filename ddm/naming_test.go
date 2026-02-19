package ddm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLegacyProfileDeclarationID(t *testing.T) {
	tests := []struct {
		name      string
		udid      string
		profileID string
		expected  string
	}{
		{
			name:      "basic identifiers",
			udid:      "ABCD-1234",
			profileID: "com.airbnb.mdm.wifi",
			expected:  "biz.airbnb.ABCD-1234.legacy_profile.com.airbnb.mdm.wifi",
		},
		{
			name:      "uuid-style udid",
			udid:      "00000000-0000-0000-0000-000000000001",
			profileID: "com.example.profile",
			expected:  "biz.airbnb.00000000-0000-0000-0000-000000000001.legacy_profile.com.example.profile",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LegacyProfileDeclarationID(tt.udid, tt.profileID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestActivationDeclarationID(t *testing.T) {
	tests := []struct {
		name      string
		udid      string
		profileID string
		expected  string
	}{
		{
			name:      "basic identifiers",
			udid:      "ABCD-1234",
			profileID: "com.airbnb.mdm.wifi",
			expected:  "biz.airbnb.ABCD-1234.legacy_profile_activation.com.airbnb.mdm.wifi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ActivationDeclarationID(tt.udid, tt.profileID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProfileIDFromPayloadIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "typical payload identifier",
			input:    "com.airbnb.mdm.wifi",
			expected: "com.airbnb.mdm.wifi",
		},
		{
			name:     "identifier with hyphens and underscores",
			input:    "com.airbnb.mdm.my-profile_v2",
			expected: "com.airbnb.mdm.my-profile_v2",
		},
		{
			name:     "identifier with spaces",
			input:    "com.airbnb.mdm.my profile",
			expected: "com.airbnb.mdm.my_profile",
		},
		{
			name:     "identifier with special characters",
			input:    "com.airbnb.mdm/profile@v1",
			expected: "com.airbnb.mdm_profile_v1",
		},
		{
			name:     "empty identifier",
			input:    "",
			expected: "",
		},
		{
			name:     "only safe characters",
			input:    "abcABC012.-_",
			expected: "abcABC012.-_",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProfileIDFromPayloadIdentifier(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProfileDownloadURL(t *testing.T) {
	tests := []struct {
		name              string
		nanoMDMURL        string
		udid              string
		payloadIdentifier string
		expected          string
	}{
		{
			name:              "basic URL",
			nanoMDMURL:        "https://nanomdm.airbnb.biz",
			udid:              "ABCD-1234",
			payloadIdentifier: "com.airbnb.mdm.wifi",
			expected:          "https://nanomdm.airbnb.biz/authproxy/profiledownload/ABCD-1234/com.airbnb.mdm.wifi",
		},
		{
			name:              "trailing slash in base URL",
			nanoMDMURL:        "https://nanomdm.airbnb.biz/",
			udid:              "ABCD-1234",
			payloadIdentifier: "com.airbnb.mdm.wifi",
			expected:          "https://nanomdm.airbnb.biz/authproxy/profiledownload/ABCD-1234/com.airbnb.mdm.wifi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProfileDownloadURL(tt.nanoMDMURL, tt.udid, tt.payloadIdentifier)
			assert.Equal(t, tt.expected, result)
		})
	}
}
