package types

import uuid "github.com/satori/go.uuid"

type DeviceFromMDM struct {
	SerialNumber     string `json:"serial_number"`
	UDID             string `json:"udid"`
	EnrollmentStatus bool   `json:"enrollment_status"`
	LastSeen         string `json:"last_seen"`
}

type DevicesFromMDM struct {
	Devices []DeviceFromMDM `json:"devices"`
}

type ScheduledPush struct {
	ID         uuid.UUID `gorm:"primary_key;type:uuid;default:uuid_generate_v4()"`
	DeviceUDID string
	Status     string `gorm:"default:'pending'"`
	Expiration int64
}
