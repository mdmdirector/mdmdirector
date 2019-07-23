package types

type DeviceInstallApplication struct {
	ID          uint `gorm:"primary_key"`
	ManifestURL string
	DeviceUDID  string
}

type SharedInstallApplication struct {
	ID          uint `gorm:"primary_key"`
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
