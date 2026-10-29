package types

import (
	"time"

	"github.com/lib/pq"
)

type Command struct {
	// idx_commands_pending covers only rows with no final result, which are a small part
	// of a table that keeps every command ever sent. It serves GET /command/pending
	// (filtered on these statuses, ordered by updated_at) and expireStaleCommands
	// (status "" and older than a cutoff) without scanning the history. Build it
	// CONCURRENTLY by hand on a large existing table (see README, Deploying).
	UpdatedAt   time.Time `gorm:"index:idx_commands_pending,priority:1,where:status = '' OR status = 'NotNow'"`
	CommandUUID string    `gorm:"primaryKey;index:idx_commands_pending,priority:2"`
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
	// AttemptCount is recorded on the Command row at insert time so a retry's row never
	// exists with the wrong count. Not sent to the MDM server.
	AttemptCount int `json:"-"`
}

type CommandResponse struct {
	Payload struct {
		CommandUUID string `json:"command_uuid"`
		Command     CommandPayload
	} `json:"payload"`
}
