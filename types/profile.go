package types

import (
	"github.com/google/uuid"

	"gorm.io/gorm"
)

// DeviceProfile (s) are profiles that are individual to the device.
type DeviceProfile struct {
	// ID                uuid.UUID `gorm:"primaryKey;type:uuid;default:uuid_generate_v4()"`
	PayloadUUID              string
	PayloadIdentifier        string `gorm:"primaryKey"`
	HashedPayloadUUID        string
	MobileconfigData         []byte
	MobileconfigHash         []byte
	OriginalMobileconfigHash []byte
	DeviceUDID               string `gorm:"primaryKey"`
	Installed                bool   `gorm:"default:true"`
}

// SharedProfile (s) are profiles that go on every device.
type SharedProfile struct {
	ID                       uuid.UUID `gorm:"primaryKey;type:uuid;default:uuid_generate_v4()"`
	PayloadUUID              string
	HashedPayloadUUID        string
	PayloadIdentifier        string
	MobileconfigData         []byte
	MobileconfigHash         []byte
	OriginalMobileconfigHash []byte
	Installed                bool `gorm:"default:true"`
}

// ProfilePayload - struct to unpack the payload sent to mdmdirector
type ProfilePayload struct {
	SerialNumbers []string `json:"serial_numbers,omitempty"`
	DeviceUDIDs   []string `json:"udids,omitempty"`
	Mobileconfigs []string `json:"profiles"`
	PushNow       bool     `json:"push_now"`
	Metadata      bool     `json:"metadata"`
}

type DeleteProfilePayload struct {
	SerialNumbers []string                     `json:"serial_numbers,omitempty"`
	DeviceUDIDs   []string                     `json:"udids,omitempty"`
	PushNow       bool                         `json:"push_now"`
	Mobileconfigs []DeletedMobileconfigPayload `json:"profiles"`
	Metadata      bool                         `json:"metadata"`
}

type DeletedMobileconfigPayload struct {
	PayloadIdentifier string `json:"payload_identifier"`
}

// ProfileList - returned data from the ProfileList MDM command
type ProfileListData struct {
	ProfileList []ProfileList
}

type ProfileList struct {
	ID uuid.UUID `gorm:"primaryKey;type:uuid;default:uuid_generate_v4()"`
	// Every ProfileList response and profile verification looks rows up by device (and
	// usually payload identifier); without this index each lookup is a sequential scan of
	// the whole table, which exhausts the DB pool under check-in load. AutoMigrate creates
	// it; on a large existing table create it CONCURRENTLY by hand before deploying (see
	// README, Deploying).
	DeviceUDID               string               `gorm:"index:idx_profile_lists_device_payload,priority:1"`
	HasRemovalPasscode       bool                 `plist:"HasRemovalPasscode"`
	IsEncrypted              bool                 `plist:"IsEncrypted"`
	IsManaged                bool                 `plist:"IsManaged"`
	PayloadContent           []PayloadContentItem `plist:"PayloadContent" gorm:"-"`
	PayloadDescription       string               `plist:"PayloadDescription"`
	PayloadDisplayName       string               `plist:"PayloadDisplayName"`
	PayloadIdentifier        string               `plist:"PayloadIdentifier" gorm:"index:idx_profile_lists_device_payload,priority:2"`
	PayloadOrganization      string               `plist:"PayloadOrganization"`
	PayloadRemovalDisallowed bool                 `plist:"PayloadRemovalDisallowed"`
	PayloadUUID              string               `plist:"PayloadUUID" gorm:"not null"`
	PayloadVersion           int                  `plist:"PayloadVersion"`
	FullPayload              bool                 `plist:"FullPayload"`
	SignerCertificates       [][]byte             `plist:"SignerCertificates" gorm:"-"`
}

type MetadataItem struct {
	ProfileMetadata []ProfileMetadata `json:"profile_metadata"`
}

type ProfileMetadata struct {
	Status            string `json:"status"`
	PayloadIdentifier string `json:"payload_identifier"`
	PayloadUUID       string `json:"payload_uuid"`
	HashedPayloadUUID string `json:"hashed_payload_uuid"`
}

// https://developer.apple.com/documentation/devicemanagement/profilelistresponse/profilelistitem/payloadcontentitem
type PayloadContentItem struct {
	PayloadDescription  string `plist:"PayloadDescription"`
	PayloadDisplayName  string `plist:"PayloadDisplayName"`
	PayloadIdentifier   string `plist:"PayloadIdentifier"`
	PayloadOrganization string `plist:"PayloadOrganization"`
	PayloadType         string `plist:"PayloadType"`
	PayloadVersion      int    `plist:"PayloadVersion"`
}

func (profile *DeviceProfile) AfterCreate(tx *gorm.DB) (err error) {
	BumpDeviceLastUpdated(profile.DeviceUDID)
	return nil
}

func (profile *DeviceProfile) AfterUpdate(tx *gorm.DB) (err error) {
	BumpDeviceLastUpdated(profile.DeviceUDID)
	return nil
}
