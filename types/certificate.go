package types

import "time"

// Certificate represents a certificate.
type Certificate struct {
	CommonName string
	Subject    string
	NotAfter   time.Time
	Data       []byte
	DeviceUDID string
}

// CertificateListData - returned data from the ProfileList MDM command
type CertificateListData struct {
	CertificateList []CertificateList
}

// CertificateList Each item from CertificateList
type CertificateList struct {
	CommonName string `plist:"CommonName"`
	Data       []byte `plist:"Data"`
	IsIdentity bool   `plist:"IsIdentity"`
}
