package types

type SecurityInfoData struct {
	SecurityInfo SecurityInfo
}

type SecurityInfo struct {
	AuthenticatedRootVolumeEnabled                   bool                   `plist:"AuthenticatedRootVolumeEnabled"`
	BootstrapTokenAllowedForAuthentication           string                 `plist:"BootstrapTokenAllowedForAuthentication"`
	BootstrapTokenRequiredForKernelExtensionApproval bool                   `plist:"BootstrapTokenRequiredForKernelExtensionApproval"`
	BootstrapTokenRequiredForSoftwareUpdate          bool                   `plist:"BootstrapTokenRequiredForSoftwareUpdate"`
	HardwareEncryptionCaps                           int                    `plist:"HardwareEncryptionCaps"`
	PasscodePresent                                  bool                   `plist:"PasscodePresent"`
	PasscodeCompliant                                bool                   `plist:"PasscodeCompliant"`
	PasscodeCompliantWithProfiles                    bool                   `plist:"PasscodeCompliantWithProfiles"`
	PasscodeLockGracePeriod                          int                    `plist:"PasscodeLockGracePeriod"`
	PasscodeLockGracePeriodEnforced                  int                    `plist:"PasscodeLockGracePeriodEnforced"`
	FDEEnabled                                       bool                   `plist:"FDE_Enabled"`
	FDEHasPersonalRecoveryKey                        bool                   `plist:"FDE_HasPersonalRecoveryKey"`
	FDEHasInstitutionalRecoveryKey                   bool                   `plist:"FDE_HasInstitutionalRecoveryKey"`
	FDEPersonalRecoveryKeyCMS                        []byte                 `plist:"FDE_PersonalRecoveryKeyCMS"`
	FDEPersonalRecoveryKeyDeviceKey                  string                 `plist:"FDE_PersonalRecoveryKeyDeviceKey" gorm:"-"`
	FirewallSettings                                 FirewallSettings       `plist:"FirewallSettings" gorm:"foreignKey:DeviceUDID"`
	SystemIntegrityProtectionEnabled                 bool                   `plist:"SystemIntegrityProtectionEnabled"`
	FirmwarePasswordStatus                           FirmwarePasswordStatus `plist:"FirmwarePasswordStatus" gorm:"foreignKey:DeviceUDID"`
	ManagementStatus                                 ManagementStatus       `plist:"ManagementStatus" gorm:"foreignKey:DeviceUDID"`
	RemoteDesktopEnabled                             bool                   `plist:"RemoteDesktopEnabled"`
	SecureBoot                                       SecureBoot             `plist:"SecureBoot" gorm:"foreignKey:DeviceUDID"`
	DeviceUDID                                       string                 `gorm:"primaryKey"`
}

type FirmwarePasswordStatus struct {
	PasswordExists bool   `plist:"PasswordExists"`
	ChangePending  bool   `plist:"ChangePending"`
	AllowOroms     bool   `plist:"AllowOroms"`
	DeviceUDID     string `gorm:"primaryKey"`
}

type ManagementStatus struct {
	EnrolledViaDEP             bool   `plist:"EnrolledViaDEP"`
	UserApprovedEnrollment     bool   `plist:"UserApprovedEnrollment"`
	IsUserEnrollment           bool   `plist:"IsUserEnrollment"`
	IsActivationLockManageable bool   `plist:"IsActivationLockManageable"`
	DeviceUDID                 string `gorm:"primaryKey"`
}

type FirewallSettings struct {
	// FirewallSettingsApplications []FirewallSettingsApplication `plist:"Applications"gorm"foreignKey:DeviceUDID"`
	BlockAllIncoming bool   `plist:"BlockAllIncoming"`
	FirewallEnabled  bool   `plist:"FirewallEnabled"`
	StealthMode      bool   `plist:"StealthMode"`
	DeviceUDID       string `gorm:"primaryKey"`
}

// type FirewallSettingsApplication struct {
// 	Allowed           bool      `plist:"Allowed"`
// 	BundleID          string    `plist:"BundleID"`
// 	Name              string    `plist:"Name"`
// 	FirewallAppItemID uuid.UUID `gorm:"primary_key;type:uuid;default:uuid_generate_v4()"`
// 	DeviceUDID        string
// }

type SecureBoot struct {
	ExternalBootLevel         string                    `plist:"ExternalBootLevel"`
	SecureBootLevel           string                    `plist:"SecureBootLevel"`
	SecureBootReducedSecurity SecureBootReducedSecurity `plist:"ReducedSecurity" gorm:"foreignKey:DeviceUDID"`
	DeviceUDID                string                    `gorm:"primaryKey"`
}

type SecureBootReducedSecurity struct {
	AllowsAnyAppleSignedOS bool   `plist:"AllowsAnyAppleSignedOS"`
	AllowsMDM              bool   `plist:"AllowsMDM"`
	AllowsUserKextApproval bool   `plist:"AllowsUserKextApproval"`
	DeviceUDID             string `gorm:"primaryKey"`
}
