package types

import (
	"time"

	"github.com/lib/pq"
)

type Command struct {
	UpdatedAt   time.Time
	CommandUUID string `gorm:"primaryKey"`
	Status      string
	DeviceUDID  string         `json:"udid"`
	RequestType string         `json:"request_type"`
	Payload     string         `json:"payload,omitempty"`
	Queries     pq.StringArray `json:"Queries,omitempty" gorm:"type:text[]"`
	Identifier  string         `json:"identifier,omitempty"`
	ManifestURL string         `json:"manifest_url,omitempty"`
	// ContentHash is the profile's HashedPayloadUUID (content-derived) at the time this
	// command was queued. InstallProfileInQueue matches on it so a profile whose content
	// changed while an InstallProfile for it was still pending gets re-enqueued.
	ContentHash  string
	ErrorString  string
	AttemptCount int
}

type CommandPayload struct {
	UDID        string   `json:"udid"`
	RequestType string   `json:"request_type"`
	Payload     string   `json:"payload,omitempty"`
	Queries     []string `json:"Queries,omitempty"`
	Identifier  string   `json:"identifier,omitempty"`
	ManifestURL string   `json:"manifest_url,omitempty"`
	Pin         string   `json:"pin,omitempty"`
	ContentHash string   `json:"-"`
}

type CommandResponse struct {
	Payload struct {
		CommandUUID string `json:"command_uuid"`
		Command     CommandPayload
	} `json:"payload"`
}
