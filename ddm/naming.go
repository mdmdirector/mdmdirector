package ddm

import (
	"fmt"
	"strings"
)

const declarationPrefix = "biz.airbnb"

// LegacyProfileDeclarationID returns the declaration identifier for a LegacyProfile
// Format: biz.airbnb.<udid>.legacy_profile.<profileID>
func LegacyProfileDeclarationID(udid, profileID string) string {
	return fmt.Sprintf("%s.%s.legacy_profile.%s", declarationPrefix, udid, profileID)
}

// ActivationDeclarationID returns the declaration identifier for an ActivationSimple
// Format: biz.airbnb.<udid>.legacy_profile_activation.<profileID>
func ActivationDeclarationID(udid, profileID string) string {
	return fmt.Sprintf("%s.%s.legacy_profile_activation.%s", declarationPrefix, udid, profileID)
}

// ProfileDownloadURL constructs the URL a device will use to fetch profile data
// The URL is routed through NanoMDM's authentication proxy to MDMDirector
func ProfileDownloadURL(nanoMDMURL, udid, payloadIdentifier string) string {
	return fmt.Sprintf("%s/authproxy/profiledownload/%s/%s", strings.TrimRight(nanoMDMURL, "/"), udid, payloadIdentifier)
}
