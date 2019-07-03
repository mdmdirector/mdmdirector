package types

type SecurityInfoData struct {
	SecurityInfo SecurityInfo
}

type SecurityInfo struct {
	HardwareEncryptionCaps          int    `plist:"HardwareEncryptionCaps"`
	PasscodePresent                 bool   `plist:"PasscodePresent"`
	PasscodeCompliant               bool   `plist:"PasscodeCompliant"`
	PasscodeCompliantWithProfiles   bool   `plist:"PasscodeCompliantWithProfiles"`
	PasscodeLockGracePeriod         int    `plist:"PasscodeLockGracePeriod"`
	PasscodeLockGracePeriodEnforced int    `plist:"PasscodeLockGracePeriodEnforced"`
	FDEEnabled                      bool   `plist:"FDE_Enabled"`
	FDEHasPersonalRecoveryKey       bool   `plist:"FDE_HasPersonalRecoveryKey"`
	FDEHasInstitutionalRecoveryKey  bool   `plist:"FDE_HasInstitutionalRecoveryKey"`
	FDEPersonalRecoveryKeyCMS       []byte `plist:"FDE_PersonalRecoveryKeyCMS"`
	FDEPersonalRecoveryKeyDeviceKey string `plist:"FDE_PersonalRecoveryKeyDeviceKey"`
	// Split this out into it's own struct
	// FirewallSettings                 interface{}     `plist:"FirewallSettings"`
	SystemIntegrityProtectionEnabled bool                   `plist:"SystemIntegrityProtectionEnabled"`
	FirmwarePasswordStatus           FirmwarePasswordStatus `plist:"FirmwarePasswordStatus" gorm:"ForeignKey:DeviceUDID"`
	ManagementStatus                 ManagementStatus       `plist:"ManagementStatus" gorm:"ForeignKey:DeviceUDID"`
	DeviceUDID                       string                 `gorm:"primary_key"`
}

type FirmwarePasswordStatus struct {
	PasswordExists bool   `plist:"PasswordExists"`
	ChangePending  bool   `plist:"ChangePending`
	AllowOroms     bool   `plist:"AllowOroms"`
	DeviceUDID     string `gorm:"primary_key"`
}

type ManagementStatus struct {
	EnrolledViaDEP         bool   `plist:"EnrolledViaDEP"`
	UserApprovedEnrollment bool   `plist:"UserApprovedEnrollment"`
	DeviceUDID             string `gorm:"primary_key"`
}
