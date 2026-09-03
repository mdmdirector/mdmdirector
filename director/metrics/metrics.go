package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const subsystem = "mdmdirector"

// UnknownLabel is the placeholder used when a metric label cannot be derived
const UnknownLabel = "unknown"

// checkinRequestsTotal counts MDM check-in events received via the webhook by message type and result
// message_type: Authenticate, TokenUpdate, CheckOut, unknown (CommandAndReportResults are tracked in commandResultsTotal)
//
//nolint:gochecknoglobals
var checkinRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "checkin_requests_total",
		Help:      "Total MDM check-in webhook events by message type and result.",
	},
	[]string{"message_type", "result"},
)

// CheckinRequests - accessor for checkinRequestsTotal
func CheckinRequests(messageType, result string) prometheus.Counter {
	return checkinRequestsTotal.WithLabelValues(messageType, result)
}

// commandResultsTotal counts MDM CommandAndReportResults webhook events by device-reported status and result
// status values: Acknowledged, Error, Idle, NotNow, CommandFormatError
//
//nolint:gochecknoglobals
var commandResultsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "command_results_total",
		Help:      "Total MDM CommandAndReportResults webhook events by device-reported status and result.",
	},
	[]string{"status", "result"},
)

// CommandResults - accessor for commandResultsTotal
func CommandResults(status, result string) prometheus.Counter {
	return commandResultsTotal.WithLabelValues(status, result)
}

// pushRequestsTotal counts APNs push requests dispatched by mdmdirector by result
//
//nolint:gochecknoglobals
var pushRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "push_requests_total",
		Help:      "Total APNs push requests dispatched by mdmdirector by result.",
	},
	[]string{"result"},
)

// PushRequests - accessor for pushRequestsTotal
func PushRequests(result string) prometheus.Counter {
	return pushRequestsTotal.WithLabelValues(result)
}

// profileOperationsTotal counts profile delivery operations dispatched by mdmdirector
// Labels:
//   - scope:     "device" (per-device profile) or "shared" (shared profile)
//   - operation: "pushed" or "deleted"
//   - result:    "success" or "error"
//
//nolint:gochecknoglobals
var profileOperationsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "profile_operations_total",
		Help:      "Total profile push/delete operations dispatched by mdmdirector, by scope, operation and result.",
	},
	[]string{"scope", "operation", "result"},
)

// ProfileOperations - accessor for profileOperationsTotal
func ProfileOperations(scope, operation, result string) prometheus.Counter {
	return profileOperationsTotal.WithLabelValues(scope, operation, result)
}

// applicationOperationsTotal counts application delivery operations dispatched by mdmdirector
// Labels:
//   - scope:     "device" (per-device app) or "shared" (shared app)
//   - operation: "pushed"
//   - result:    "success" or "error"
//
//nolint:gochecknoglobals
var applicationOperationsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "application_operations_total",
		Help:      "Total application push operations dispatched by mdmdirector, by scope, operation and result.",
	},
	[]string{"scope", "operation", "result"},
)

// ApplicationOperations - accessor for applicationOperationsTotal
func ApplicationOperations(scope, operation, result string) prometheus.Counter {
	return applicationOperationsTotal.WithLabelValues(scope, operation, result)
}

// enqueueRequestsTotal counts MDM command enqueue attempts dispatched by mdmdirector
// command_type: e.g. InstallProfile, RemoveProfile, InstallApplication, DeviceInformation,
// ProfileList, SecurityInfo, CertificateList, DeviceLock, EraseDevice
//
//nolint:gochecknoglobals
var enqueueRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "enqueue_requests_total",
		Help:      "Total MDM command enqueue attempts by mdmdirector, by command type and result.",
	},
	[]string{"command_type", "result"},
)

// EnqueueRequests - accessor for enqueueRequestsTotal
func EnqueueRequests(commandType, result string) prometheus.Counter {
	return enqueueRequestsTotal.WithLabelValues(commandType, result)
}

// profileVerificationMismatchesTotal counts profiles found missing from a device's reported ProfileList during verification
// scope: "device" or "shared"
//
//nolint:gochecknoglobals
var profileVerificationMismatchesTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "profile_verification_mismatches_total",
		Help:      "Total profile verification mismatches detected (desired profile absent from device's reported ProfileList), by scope.",
	},
	[]string{"scope"},
)

// ProfileVerificationMismatches - accessor for profileVerificationMismatchesTotal
func ProfileVerificationMismatches(scope string) prometheus.Counter {
	return profileVerificationMismatchesTotal.WithLabelValues(scope)
}

// profileDownloadRequestsTotal counts /profiledownload requests made by devices fetching
// the mobileconfig referenced by a DDM legacy-profile declaration's ProfileURL
// Labels:
//   - scope:   "device" (served from device_profiles), "shared" (served from shared_profiles),
//     "none" (nothing served)
//   - outcome: "success", "unauthorized" (X-Enrollment-ID mismatch), "not_found"
//     (dangling declaration - no installed profile for the identifier), "error"
//
//nolint:gochecknoglobals
var profileDownloadRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "profile_download_requests_total",
		Help:      "Total /profiledownload requests served to devices, by scope and outcome.",
	},
	[]string{"scope", "outcome"},
)

// ProfileDownloadRequests - accessor for profileDownloadRequestsTotal
func ProfileDownloadRequests(scope, outcome string) prometheus.Counter {
	return profileDownloadRequestsTotal.WithLabelValues(scope, outcome)
}

// profileAPIRequestsTotal counts operator-driven requests to the /profile API
// Labels:
//   - method: "post" or "delete"
//   - result: "success" or "error"
//
//nolint:gochecknoglobals
var profileAPIRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "profile_api_requests_total",
		Help:      "Total /profile API requests by method and result.",
	},
	[]string{"method", "result"},
)

// ProfileAPIRequests - accessor for profileAPIRequestsTotal
func ProfileAPIRequests(method, result string) prometheus.Counter {
	return profileAPIRequestsTotal.WithLabelValues(method, result)
}

// initialTasksTotal counts RunInitialTasks invocations by terminal result
// result: "success", "error", "lease_contention"
//
//nolint:gochecknoglobals
var initialTasksTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "initial_tasks_total",
		Help:      "Total RunInitialTasks invocations by terminal result.",
	},
	[]string{"result"},
)

// InitialTasks - accessor for initialTasksTotal
func InitialTasks(result string) prometheus.Counter {
	return initialTasksTotal.WithLabelValues(result)
}

// ddmDeclarationWritesTotal counts KMFDDM declaration write operations issued by mdmdirector
// Labels:
//   - declaration_type:    "configuration", "activation", "asset", "management", "unknown"
//   - declaration_subtype: "profile" or "application" for configuration declarations; "" otherwise
//   - operation:           "put", "touch", or "delete"
//   - result:              "success" or "error"
//
//nolint:gochecknoglobals
var ddmDeclarationWritesTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "ddm_declaration_writes_total",
		Help:      "Total KMFDDM declaration writes issued by mdmdirector, by declaration type/subtype, operation and result.",
	},
	[]string{"declaration_type", "declaration_subtype", "operation", "result"},
)

// DDMDeclarationWrites - accessor for ddmDeclarationWritesTotal
func DDMDeclarationWrites(declarationType, declarationSubtype, operation, result string) prometheus.Counter {
	return ddmDeclarationWritesTotal.WithLabelValues(declarationType, declarationSubtype, operation, result)
}

// ddmSetMembershipChangesTotal counts KMFDDM set-declaration membership changes issued by mdmdirector
// Labels match ddmDeclarationWritesTotal with operation values "put" or "delete".
//
//nolint:gochecknoglobals
var ddmSetMembershipChangesTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "ddm_set_membership_changes_total",
		Help:      "Total KMFDDM set-declaration membership changes issued by mdmdirector, by declaration type/subtype, operation and result.",
	},
	[]string{"declaration_type", "declaration_subtype", "operation", "result"},
)

// DDMSetMembershipChanges - accessor for ddmSetMembershipChangesTotal
func DDMSetMembershipChanges(declarationType, declarationSubtype, operation, result string) prometheus.Counter {
	return ddmSetMembershipChangesTotal.WithLabelValues(declarationType, declarationSubtype, operation, result)
}

// ddmNotifyTotal counts KMFDDM enrollment notify calls issued by mdmdirector
// result: "success", "error"
//
//nolint:gochecknoglobals
var ddmNotifyTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "ddm_notify_total",
		Help:      "Total KMFDDM enrollment notify calls issued by mdmdirector, by result.",
	},
	[]string{"result"},
)

// DDMNotify - accessor for ddmNotifyTotal
func DDMNotify(result string) prometheus.Counter {
	return ddmNotifyTotal.WithLabelValues(result)
}

// pinEscrowTotal counts unlock-pin escrow attempts to the Crypt-compatible endpoint
// result: "success", "error"
//
//nolint:gochecknoglobals
var pinEscrowTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "pin_escrow_total",
		Help:      "Total unlock-pin escrow attempts by result.",
	},
	[]string{"result"},
)

// PinEscrow - accessor for pinEscrowTotal
func PinEscrow(result string) prometheus.Counter {
	return pinEscrowTotal.WithLabelValues(result)
}

// devicesTotal reports the current number of devices known to mdmdirector
//
//nolint:gochecknoglobals
var devicesTotal = promauto.NewGauge(
	prometheus.GaugeOpts{
		Subsystem: subsystem,
		Name:      "devices_total",
		Help:      "Current number of devices known to mdmdirector.",
	},
)

// DevicesTotal - accessor for devicesTotal
func DevicesTotal() prometheus.Gauge {
	return devicesTotal
}

// profilesTotal reports the current number of profiles tracked by mdmdirector
// Labels:
//   - scope:     "device" or "shared"
//   - installed: "true" or "false"
//
//nolint:gochecknoglobals
var profilesTotal = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Subsystem: subsystem,
		Name:      "profiles_total",
		Help:      "Current number of profiles tracked by mdmdirector, by scope and installed state.",
	},
	[]string{"scope", "installed"},
)

// ProfilesTotal - accessor for profilesTotal
func ProfilesTotal(scope, installed string) prometheus.Gauge {
	return profilesTotal.WithLabelValues(scope, installed)
}

// ddmEnabledDevicesTotal reports the current number of devices explicitly opted into DDM
//
//nolint:gochecknoglobals
var ddmEnabledDevicesTotal = promauto.NewGauge(
	prometheus.GaugeOpts{
		Subsystem: subsystem,
		Name:      "ddm_enabled_devices_total",
		Help:      "Current number of devices explicitly opted into DDM.",
	},
)

// DDMEnabledDevices - accessor for ddmEnabledDevicesTotal
func DDMEnabledDevices() prometheus.Gauge {
	return ddmEnabledDevicesTotal
}

// signingCertNotAfter reports the expiry (NotAfter) of the profile signing certificate as
// a Unix timestamp, so alerting can warn before it lapses.
//
// This exists because expiry is otherwise SILENT: SignProfile signs via pkcs7, which does
// not validate NotAfter, so an expired certificate keeps signing successfully and no error
// counter moves. The only downstream evidence is devices rejecting the profiles.
//
//nolint:gochecknoglobals
var signingCertNotAfter = promauto.NewGauge(
	prometheus.GaugeOpts{
		Subsystem: subsystem,
		Name:      "signing_cert_not_after_timestamp_seconds",
		Help:      "Expiry (NotAfter) of the loaded profile signing certificate, as a Unix timestamp.",
	},
)

// SigningCertNotAfter - accessor for signingCertNotAfter
func SigningCertNotAfter() prometheus.Gauge {
	return signingCertNotAfter
}

// ResultLabel maps an HTTP status code or error presence to the canonical result label.
func ResultLabel(statusCode int) string {
	if statusCode >= 400 {
		return "error"
	}
	return "success"
}

// ResultFromError returns "success" if err is nil, otherwise "error".
func ResultFromError(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
}

// reenrollSkippedTotal counts re-enrollment attempts that were suppressed because the device
// cannot complete the enrollment profile mdmenroll would return. An Intel Mac, or one whose
// model is not yet known, cannot do ACME against stepca, so pushing it a fresh enrollment
// profile can only fail or re-enroll it into the legacy stack.
// Labels:
//   - trigger: "cert_expiry" (validateEnrollmentCertExpiry) or "signer_mismatch"
//     (ensureCertOnEnrollmentProfile)
//   - arch:    "intel" or "unknown"
//
//nolint:gochecknoglobals
var reenrollSkippedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "reenroll_skipped_total",
		Help:      "Total re-enrollments suppressed because the device architecture cannot complete ACME enrollment, by trigger and architecture.",
	},
	[]string{"trigger", "arch"},
)

// ReenrollSkipped - accessor for reenrollSkippedTotal
func ReenrollSkipped(trigger, arch string) prometheus.Counter {
	return reenrollSkippedTotal.WithLabelValues(trigger, arch)
}
