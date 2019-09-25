package types

import "time"

type Command struct {
	UpdatedAt   time.Time
	CommandUUID string `gorm:"primary_key"`
	Status      string
	DeviceUDID  string
	RequestType string
	Data        string
	ErrorString string
}

type CommandPayload struct {
	UDID        string   `json:"udid"`
	RequestType string   `json:"request_type"`
	Payload     string   `json:"payload,omitempty"`
	Queries     []string `json:"Queries,omitempty"`
	Identifier  string   `json:"identifier,omitempty"`
	ManifestURL string   `json:"manifest_url,omitempty"`
}

type CommandResponse struct {
	Payload struct {
		CommandUUID string `json:"command_uuid"`
		Command     CommandPayload
	} `json:"payload"`
}
