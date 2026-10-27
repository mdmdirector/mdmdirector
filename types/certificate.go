package types

import (
	"time"

	"github.com/google/uuid"
)

// Certificate represents a certificate.
type Certificate struct {
	ID         uuid.UUID `gorm:"primaryKey;type:uuid;default:uuid_generate_v4()"`
	CommonName string
	Subject    string
	NotAfter   time.Time
	NotBefore  time.Time
	Data       []byte
	Issuer     string
	// Every CertificateList response replaces the device's rows (Association Replace) and
	// GetDevice preloads them, both by device_ud_id; without this each was a full scan.
	DeviceUDID string `gorm:"index:idx_certificates_device"`
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
