package types

import (
	"time"
)

type Command struct {
	UpdatedAt   time.Time
	CommandUUID string `gorm:"primary_key"`
	Status      string
	DeviceUDID  string
	RequestType string
}

func (command *Command) AfterCreate() (err error) {
	BumpDeviceLastUpdated(command.DeviceUDID)
	return
}

func (command *Command) AfterUpdate() (err error) {
	BumpDeviceLastUpdated(command.DeviceUDID)
	return
}

type CommandPayload struct {
	UDID        string   `json:"udid"`
	RequestType string   `json:"request_type"`
	Payload     string   `json:"payload"`
	Queries     []string `json:"Queries"`
	Identifier  string   `json:"identifier"`
}

type CommandResponse struct {
	Payload struct {
		CommandUUID string `json:"command_uuid"`
		Command     CommandPayload
	} `json:"payload"`
}
