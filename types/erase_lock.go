package types

import (
	"time"

	"github.com/google/uuid"
)

type EscrowPayload struct {
	Serial     string `form:"serial"`
	Pin        string `form:"recovery_password"`
	Username   string `form:"username"`
	SecretType string `form:"secret_type"`
}

type UnlockPin struct {
	ID         uuid.UUID `gorm:"primaryKey;type:uuid;default:uuid_generate_v4()"`
	UnlockPin  string
	PinSet     time.Time
	DeviceUDID string
}
