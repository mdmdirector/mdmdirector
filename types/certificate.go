package types

import "time"

// Certificate represents a certificate.
type Certificate struct {
	CommonName                      string    `plist:"CommonName"`
	Subject                         string    `plist:"Subject"`
	NotAfter                        time.Time `plist:"PasscodePresent"`
	PasscodeCompliant               bool      `plist:"PasscodeCompliant"`
	PasscodeCompliantWithProfiles   bool      `plist:"PasscodeCompliantWithProfiles"`
	PasscodeLockGracePeriod         int       `plist:"PasscodeLockGracePeriod"`
	PasscodeLockGracePeriodEnforced int       `plist:"PasscodeLockGracePeriodEnforced"`
	FDEEnabled                      bool      `plist:"FDE_Enabled"`
	FDEHasPersonalRecoveryKey       bool      `plist:"FDE_HasPersonalRecoveryKey"`
	FDEHasInstitutionalRecoveryKey  bool      `plist:"FDE_HasInstitutionalRecoveryKey"`
	FDEPersonalRecoveryKeyCMS       []byte    `plist:"FDE_PersonalRecoveryKeyCMS"`
	FDEPersonalRecoveryKeyDeviceKey string    `plist:"FDE_PersonalRecoveryKeyDeviceKey"`
	// Split this out into it's own struct
	// FirewallSettings                 interface{}     `plist:"FirewallSettings"`
	SystemIntegrityProtectionEnabled bool                   `plist:"SystemIntegrityProtectionEnabled"`
	FirmwarePasswordStatus           FirmwarePasswordStatus `plist:"FirmwarePasswordStatus" gorm:"ForeignKey:DeviceUDID"`
	ManagementStatus                 ManagementStatus       `plist:"ManagementStatus" gorm:"ForeignKey:DeviceUDID"`
	DeviceUDID                       string                 `gorm:"primary_key"`
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
