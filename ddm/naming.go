package ddm

import (
	"fmt"
	"strings"
)

// withPrefix joins prefix and name with a dot. An empty prefix leaves name as is.
func withPrefix(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// DeviceSetName returns the name of a device's KMFDDM set, which holds every
// declaration MDMDirector delivers to that device.
// Format: <prefix>.<udid>, or <udid> with no prefix
func DeviceSetName(prefix, udid string) string {
	return withPrefix(prefix, udid)
}

// LegacyProfileDeclarationID returns the declaration identifier for a LegacyProfile
// Format: <prefix>.<udid>.legacy_profile.<profileID>, or without <prefix>.
func LegacyProfileDeclarationID(prefix, udid, profileID string) string {
	return withPrefix(prefix, fmt.Sprintf("%s.legacy_profile.%s", udid, profileID))
}

// ProfileActivationDeclarationID returns the declaration identifier for an ActivationSimple for Profile
// Format: <prefix>.<udid>.legacy_profile_activation.<profileID>, or without <prefix>.
func ProfileActivationDeclarationID(prefix, udid, profileID string) string {
	return withPrefix(prefix, fmt.Sprintf("%s.legacy_profile_activation.%s", udid, profileID))
}

// ProfileDownloadURL constructs the URL a device will use to fetch profile data
// The URL is routed through NanoMDM's authentication proxy to MDMDirector
func ProfileDownloadURL(nanoMDMURL, udid, payloadIdentifier string) string {
	return fmt.Sprintf("%s/authproxy/profiledownload/%s/%s", strings.TrimRight(nanoMDMURL, "/"), udid, payloadIdentifier)
}

// PackageDeclarationID returns the declaration identifier for a Package declaration
// Format: <prefix>.<udid>.package.<packageUUID>, or without <prefix>.
func PackageDeclarationID(prefix, udid, packageUUID string) string {
	return withPrefix(prefix, fmt.Sprintf("%s.package.%s", udid, packageUUID))
}

// PackageActivationDeclarationID returns the declaration identifier for the ActivationSimple for Package
// Format: <prefix>.<udid>.package_activation.<packageUUID>, or without <prefix>.
func PackageActivationDeclarationID(prefix, udid, packageUUID string) string {
	return withPrefix(prefix, fmt.Sprintf("%s.package_activation.%s", udid, packageUUID))
}
