package types

type EscrowPayload struct {
	Serial     string `form:"serial"`
	Pin        string `form:"recovery_password"`
	Username   string `form:"username"`
	SecretType string `form:"secret_type"`
}
