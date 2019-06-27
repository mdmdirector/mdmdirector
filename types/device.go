package types

import "time"

type Device struct {
	DeviceName   string
	BuildVersion string
	ModelName    string
	Model        string
	OSVersion    string
	ProductName  string
	SerialNumber string
	UDID         string `gorm:"primary_key"`
	Active       bool
	Profiles     []DeviceProfile `gorm:"ForeignKey:DeviceUDID"`
	Commands     []Command       `gorm:"ForeignKey:DeviceUDID"`
	UpdatedAt time.Time
}
