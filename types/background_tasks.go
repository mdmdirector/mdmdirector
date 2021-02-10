package types

type DeviceFromMDM struct {
	SerialNumber     string `json:"serial_number"`
	UDID             string `json:"udid"`
	EnrollmentStatus bool   `json:"enrollment_status"`
	LastSeen         string `json:"last_seen"`
}

type DevicesFromMDM struct {
	Devices []DeviceFromMDM `json:"devices"`
}
