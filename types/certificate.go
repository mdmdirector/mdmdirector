package types

import (
	"time"

	"github.com/google/uuid"
)

// Certificate represents a certificate.
type Certificate struct {
	ID         uuid.UUID `gorm:"primary_key;type:uuid;default:uuid_generate_v4()"`
	CommonName string
	Subject    string
	NotAfter   time.Time
	Data       []byte
	DeviceUDID string
}

// CertificateListData - returned data from the CertificateList MDM command
type CertificateListData struct {
	CertificateList []CertificateList
}

// CertificateList Each item from CertificateList
type CertificateList struct {
	CommonName string `plist:"CommonName"`
	Data       []byte `plist:"Data"`
	IsIdentity bool   `plist:"IsIdentity"`
}
