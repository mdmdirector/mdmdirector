package ddm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeviceSetName(t *testing.T) {
	assert.Equal(t, "com.example.00000000-0000-0000-0000-000000000001", DeviceSetName("com.example", "00000000-0000-0000-0000-000000000001"))
	assert.Equal(t, "00000000-0000-0000-0000-000000000001", DeviceSetName("", "00000000-0000-0000-0000-000000000001"))
}

// An empty prefix leaves no leading dot.
func TestDeclarationIDsWithoutPrefix(t *testing.T) {
	assert.Equal(t, "ABCD-1234.legacy_profile.com.example.wifi", LegacyProfileDeclarationID("", "ABCD-1234", "com.example.wifi"))
	assert.Equal(t, "ABCD-1234.legacy_profile_activation.com.example.wifi", ProfileActivationDeclarationID("", "ABCD-1234", "com.example.wifi"))
	assert.Equal(t, "ABCD-1234.package.p-1", PackageDeclarationID("", "ABCD-1234", "p-1"))
	assert.Equal(t, "ABCD-1234.package_activation.p-1", PackageActivationDeclarationID("", "ABCD-1234", "p-1"))
}

func TestLegacyProfileDeclarationID(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		udid      string
		profileID string
		expected  string
	}{
		{
			name:      "basic identifiers",
			prefix:    "com.example",
			udid:      "ABCD-1234",
			profileID: "com.example.mdm.wifi",
			expected:  "com.example.ABCD-1234.legacy_profile.com.example.mdm.wifi",
		},
		{
			name:      "uuid-style udid",
			prefix:    "com.example",
			udid:      "00000000-0000-0000-0000-000000000001",
			profileID: "com.example.profile",
			expected:  "com.example.00000000-0000-0000-0000-000000000001.legacy_profile.com.example.profile",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LegacyProfileDeclarationID(tt.prefix, tt.udid, tt.profileID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestActivationDeclarationID(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		udid      string
		profileID string
		expected  string
	}{
		{
			name:      "basic identifiers",
			prefix:    "com.example",
			udid:      "ABCD-1234",
			profileID: "com.example.mdm.wifi",
			expected:  "com.example.ABCD-1234.legacy_profile_activation.com.example.mdm.wifi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProfileActivationDeclarationID(tt.prefix, tt.udid, tt.profileID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPackageDeclarationID(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		udid        string
		packageUUID string
		expected    string
	}{
		{
			name:        "basic identifiers",
			prefix:      "com.example",
			udid:        "ABCD-1234",
			packageUUID: "550e8400-e29b-41d4-a716-446655440000",
			expected:    "com.example.ABCD-1234.package.550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:        "uuid-style udid",
			prefix:      "com.example",
			udid:        "00000000-0000-0000-0000-000000000001",
			packageUUID: "550e8400-e29b-41d4-a716-446655440000",
			expected:    "com.example.00000000-0000-0000-0000-000000000001.package.550e8400-e29b-41d4-a716-446655440000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PackageDeclarationID(tt.prefix, tt.udid, tt.packageUUID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPackageActivationDeclarationID(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		udid        string
		packageUUID string
		expected    string
	}{
		{
			name:        "basic identifiers",
			prefix:      "com.example",
			udid:        "ABCD-1234",
			packageUUID: "550e8400-e29b-41d4-a716-446655440000",
			expected:    "com.example.ABCD-1234.package_activation.550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:        "uuid-style udid",
			prefix:      "com.example",
			udid:        "00000000-0000-0000-0000-000000000001",
			packageUUID: "550e8400-e29b-41d4-a716-446655440000",
			expected:    "com.example.00000000-0000-0000-0000-000000000001.package_activation.550e8400-e29b-41d4-a716-446655440000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PackageActivationDeclarationID(tt.prefix, tt.udid, tt.packageUUID)
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
			nanoMDMURL:        "https://mdm.example.com",
			udid:              "ABCD-1234",
			payloadIdentifier: "com.example.mdm.wifi",
			expected:          "https://mdm.example.com/authproxy/profiledownload/ABCD-1234/com.example.mdm.wifi",
		},
		{
			name:              "trailing slash in base URL",
			nanoMDMURL:        "https://mdm.example.com/",
			udid:              "ABCD-1234",
			payloadIdentifier: "com.example.mdm.wifi",
			expected:          "https://mdm.example.com/authproxy/profiledownload/ABCD-1234/com.example.mdm.wifi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProfileDownloadURL(tt.nanoMDMURL, tt.udid, tt.payloadIdentifier)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDeviceDeclarationPrefix(t *testing.T) {
	assert.Equal(t, "com.example.ABCD-1234.", DeviceDeclarationPrefix("com.example", "ABCD-1234"))
	assert.Equal(t, "ABCD-1234.", DeviceDeclarationPrefix("", "ABCD-1234"))
	// Every ID the package builds for the device carries the prefix
	assert.True(t, strings.HasPrefix(LegacyProfileDeclarationID("p", "ABCD-1234", "x"), DeviceDeclarationPrefix("p", "ABCD-1234")))
	assert.True(t, strings.HasPrefix(PackageActivationDeclarationID("", "ABCD-1234", "x"), DeviceDeclarationPrefix("", "ABCD-1234")))
}
