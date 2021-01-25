package types

import "github.com/google/uuid"

type DeviceInstallApplication struct {
	ID          uuid.UUID `gorm:"primaryKey;type:uuid;default:uuid_generate_v4()"`
	ManifestURL string
	DeviceUDID  string
}

type SharedInstallApplication struct {
	ID          uuid.UUID `gorm:"primaryKey;type:uuid;default:uuid_generate_v4()"`
	ManifestURL string
}

type InstallApplicationPayload struct {
	SerialNumbers []string      `json:"serial_numbers,omitempty"`
	DeviceUDIDs   []string      `json:"udids,omitempty"`
	ManifestURLs  []ManifestURL `json:"manifest_urls"`
}

type ManifestURL struct {
	URL           string `json:"url"`
	BootstrapOnly bool   `json:"bootstrap_only"`
}
