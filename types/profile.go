package types

// DeviceProfile (s) are profiles that are individual to the device.
type DeviceProfile struct {
	ID                uint `gorm:"primary_key"`
	PayloadUUID       string
	PayloadIdentifier string
	MobileconfigData  []byte
	MobileconfigHash  []byte
	DeviceUDID        string
}

// SharedProfile (s) are profiles that go on every device.
type SharedProfile struct {
	ID                uint `gorm:"primary_key"`
	PayloadUUID       string
	PayloadIdentifier string
	MobileconfigData  []byte
	MobileconfigHash  []byte
}

// ProfilePayload - struct to unpack the payload sent to mdmdirector
type ProfilePayload struct {
	SerialNumbers []string `json:"serial_numbers,omitempty"`
	DeviceUUIDs   []string `json:"udids,omitempty"`
	Mobileconfigs []string `json:"profiles"`
}

// ProfileList - returned data from the ProfileList MDM command
type ProfileListData struct {
	ProfileList []ProfileList
}

type ProfileList struct {
	HasRemovalPasscode       bool          `plist:"HasRemovalPasscode"`
	IsEncrypted              bool          `plist:"IsEncrypted"`
	PayloadContent           []interface{} `plist:"PaylodContent"`
	PayloadDescription       string        `plist:"PayloadDescription"`
	PayloadDisplayName       string        `plist:"PayloadDisplayName"`
	PayloadIdentifier        string        `plist:"PayloadIdentifier"`
	PayloadOrganization      string        `plist:"PayloadOrganization"`
	PayloadRemovalDisallowed bool          `plist:"PayloadRemovalDisallowed"`
	PayloadUUID              string        `plist:"PayloadUUID"`
	PayloadVersion           int           `plist:"PayloadVersion"`
	FullPayload              bool          `plist:"FullPayload"`
}
