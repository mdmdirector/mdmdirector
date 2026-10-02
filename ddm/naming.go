package ddm

import (
	"fmt"
	"strings"
)

// LegacyProfileDeclarationID returns the declaration identifier for a LegacyProfile
// Format: <prefix>.<udid>.legacy_profile.<profileID>
func LegacyProfileDeclarationID(prefix, udid, profileID string) string {
	return fmt.Sprintf("%s.%s.legacy_profile.%s", prefix, udid, profileID)
}

// ProfileActivationDeclarationID returns the declaration identifier for an ActivationSimple for Profile
// Format: <prefix>.<udid>.legacy_profile_activation.<profileID>
func ProfileActivationDeclarationID(prefix, udid, profileID string) string {
	return fmt.Sprintf("%s.%s.legacy_profile_activation.%s", prefix, udid, profileID)
}

// ProfileDownloadURL constructs the URL a device will use to fetch profile data
// The URL is routed through NanoMDM's authentication proxy to MDMDirector
func ProfileDownloadURL(nanoMDMURL, udid, payloadIdentifier string) string {
	return fmt.Sprintf("%s/authproxy/profiledownload/%s/%s", strings.TrimRight(nanoMDMURL, "/"), udid, payloadIdentifier)
}

// PackageDeclarationID returns the declaration identifier for a Package declaration
// Format: <prefix>.<udid>.package.<packageUUID>
func PackageDeclarationID(prefix, udid, packageUUID string) string {
	return fmt.Sprintf("%s.%s.package.%s", prefix, udid, packageUUID)
}

// PackageActivationDeclarationID returns the declaration identifier for the ActivationSimple for Package
// Format: <prefix>.<udid>.package_activation.<packageUUID>
func PackageActivationDeclarationID(prefix, udid, packageUUID string) string {
	return fmt.Sprintf("%s.%s.package_activation.%s", prefix, udid, packageUUID)
}
