package types

import "time"

type Device struct {
	DeviceName                       string
	BuildVersion                     string
	ModelName                        string
	Model                            string
	OSVersion                        string
	ProductName                      string
	SerialNumber                     string
	DeviceCapacity                   float32
	AvailableDeviceCapacity          float32
	BatteryLevel                     float32
	CellularTechnology               int
	IMEI                             string
	MEID                             string
	ModemFirmwareVersion             string
	IsSupervised                     bool
	IsDeviceLocatorServiceEnabled    bool
	IsActivationLockEnabled          bool
	IsDoNotDisturbInEffect           bool
	DeviceID                         string
	EASDeviceIdentifier              string
	IsCloudBackupEnabled             bool
	OSUpdateSettings                 OSUpdateSettings `gorm:"ForeignKey:DeviceUDID"`
	LocalHostName                    string
	HostName                         string
	SystemIntegrityProtectionEnabled bool
	// ActiveManagedUsers               []string
	AppAnalyticsEnabled bool
	// AutoSetupAdminAccounts interface
	IsMDMLostModeEnabled        bool
	AwaitingConfiguration       bool `gorm:"default:false"`
	MaximumResidentUsers        int
	BluetoothMAC                string
	CarrierSettingsVersion      string
	CurrentCarrierNetwork       string
	CurrentMCC                  string
	CurrentMNC                  string
	DataRoamingEnabled          string
	DiagnosticSubmissionEnabled bool
	ICCID                       string
	IsMultiUser                 bool
	IsNetworkTethered           bool
	IsRoaming                   bool
	iTunesStoreAccountHash      string
	iTunesStoreAccountIsActive  bool
	LastCloudBackupDate         time.Time
	MDMOptions                  string
	// EthernetMACs []string
	// OrganizationInfo interface{}
	PersonalHotspotEnabled bool
	PhoneNumber            string
	PushToken              string
	// ServiceSubscriptions interface{}
	SIMCarrierNetwork        string
	SIMMCC                   string
	SIMMNC                   string
	SubscriberCarrierNetwork string
	SubscriberMCC            string
	SubscriberMNC            string
	VoiceRoamingEnabled      bool
	WiFiMAC                  string
	EthernetMAC              string
	UDID                     string `gorm:"primary_key"`
	Active                   bool
	Profiles                 []DeviceProfile            `gorm:"ForeignKey:DeviceUDID"`
	Commands                 []Command                  `gorm:"ForeignKey:DeviceUDID"`
	InstallApplications      []DeviceInstallApplication `gorm:"ForeignKey:DeviceUDID"`
	SecurityInfo             SecurityInfo               `gorm:"ForeignKey:DeviceUDID"`
	UpdatedAt                time.Time
	AuthenticateRecieved     bool `gorm:"default:false"`
	TokenUpdateRecieved      bool `gorm:"default:false"`
	InitialTasksRun          bool `gorm:"default:false"`
}

var DeviceInformationQueries = []string{"ActiveManagedUsers", "AppAnalyticsEnabled", "AutoSetupAdminAccounts", "AvailableDeviceCapacity", "AwaitingConfiguration", "BatteryLevel", "BluetoothMAC", "BuildVersion", "CarrierSettingsVersion", "CellularTechnology", "CurrentMCC", "CurrentMNC", "DataRoamingEnabled", "DeviceCapacity", "DeviceID", "DeviceName", "DiagnosticSubmissionEnabled", "EASDeviceIdentifier", "ICCID", "IMEI", "IsActivationLockEnabled", "IsCloudBackupEnabled", "IsDeviceLocatorServiceEnabled", "IsDoNotDisturbInEffect", "IsMDMLostModeEnabled", "IsMultiUser", "IsNetworkTethered", "IsRoaming", "IsSupervised", "iTunesStoreAccountHash", "iTunesStoreAccountIsActive", "LastCloudBackupDate", "MaximumResidentUsers", "MDMOptions", "MEID", "Model", "ModelName", "ModemFirmwareVersion", "OrganizationInfo", "OSUpdateSettings", "OSVersion", "PersonalHotspotEnabled", "PhoneNumber", "ProductName", "PushToken", "SerialNumber", "ServiceSubscriptions", "SIMCarrierNetwork", "SIMMCC", "SIMMNC", "SubscriberCarrierNetwork", "SubscriberMCC", "SubscriberMNC", "SystemIntegrityProtectionEnabled", "UDID", "VoiceRoamingEnabled", "WiFiMAC", "EthernetMAC"}

type OSUpdateSettings struct {
	DeviceUDID                      string `gorm:"primary_key"`
	CatalogURL                      string
	IsDefaultCatalog                bool
	PreviousScanDate                time.Time
	PreviousScanResult              int
	PerformPeriodicCheck            bool
	AutomaticCheckEnabled           bool
	BackgroundDownloadEnabled       bool
	AutomaticAppInstallationEnabled bool
	AutomaticOSInstallationEnabled  bool
	AutomaticSecurityUpdatesEnabled bool
}

type DeviceInformationQueryResponses struct {
	QueryResponses Device `plist:"QueryResponses"`
}
