package types

import (
	"time"

	"github.com/lib/pq"
)

type Command struct {
	UpdatedAt   time.Time
	CommandUUID string `gorm:"primaryKey"`
	Status      string
	// Every lookup on this table is scoped to a device and usually a request type
	// (CommandInQueue, InstallProfileInQueue, erroredInstallProfiles, ClearCommands), and
	// the table keeps a row for every command ever sent, so without this index each of
	// those is a sequential scan of the whole table. AutoMigrate creates it; on a large
	// existing table create it CONCURRENTLY by hand before deploying so startup doesn't
	// block writes while it builds (see README, Deploying).
	DeviceUDID  string         `json:"udid" gorm:"index:idx_commands_device_request,priority:1"`
	RequestType string         `json:"request_type" gorm:"index:idx_commands_device_request,priority:2"`
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
