package types

import "github.com/jinzhu/gorm"

type Command struct {
	Model       gorm.Model
	CommandUUID string `gorm:"primary_key"`
	Status      string
	DeviceUDID  string
}

type CommandPayload struct {
	UDID        string `json:"udid"`
	RequestType string `json:"request_type"`
	Payload     string `json:"payload"`
}

type CommandResponse struct {
	Payload struct {
		CommandUUID string `json:"command_uuid"`
		Command     CommandPayload
	} `json:"payload"`
}
