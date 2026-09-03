package director

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"flag"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mdmdirector/mdmdirector/types"
	"github.com/micromdm/go4/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	certTestScepIssuerCN = "Test SCEP CA"
	certTestAcmeIssuerCN = "Test ACME Intermediate CA"
	certTestSerial       = "TESTSERIAL01"
	certTestUDID         = "00000000-0000-0000-0000-000000000001"
)

// registerCertFlags registers the flags validateEnrollmentCertExpiry and the webhook
// re-enrollment path read.
//
// It deliberately registers only what these tests need instead of reusing
// registerEnrollmentProfileFlags: that helper also registers "micromdmurl", which
// command_test.go registers unguarded, and this file sorts first in the package - so
// claiming that flag here would panic the later test.
func registerCertFlags(t *testing.T) {
	t.Helper()
	if flag.Lookup("enable-reenroll-via-webhook") == nil {
		flag.Bool("enable-reenroll-via-webhook", env.Bool("ENABLE_REENROLL_VIA_WEBHOOK", false), "Enable webhook-based re-enrollment.")
	}
	if flag.Lookup("enroll-webhook-url") == nil {
		flag.String("enroll-webhook-url", env.String("ENROLL_WEBHOOK_URL", ""), "Enrollment profile webhook URL.")
	}
	if flag.Lookup("enroll-webhook-token") == nil {
		flag.String("enroll-webhook-token", env.String("ENROLL_WEBHOOK_TOKEN", ""), "Enrollment profile webhook bearer token.")
	}
	if flag.Lookup("scep-cert-issuer") == nil {
		flag.String("scep-cert-issuer", env.String("SCEP_CERT_ISSUER", ""), "SCEP cert issuer.")
	}
	if flag.Lookup("scep-cert-min-validity") == nil {
		flag.Int("scep-cert-min-validity", env.Int("SCEP_CERT_MIN_VALIDITY", 180), "SCEP min validity in days.")
	}
	if flag.Lookup("acme-cert-issuer") == nil {
		flag.String("acme-cert-issuer", env.String("ACME_CERT_ISSUER", ""), "ACME cert issuer.")
	}
	if flag.Lookup("acme-cert-min-validity") == nil {
		flag.Int("acme-cert-min-validity", env.Int("ACME_CERT_MIN_VALIDITY", 180), "ACME min validity in days.")
	}
}

// issuerString renders a certificate's issuer exactly as x509 does, so the issuer the
// tests configure matches what the code compares against.
func issuerString(t *testing.T, der []byte) string {
	t.Helper()
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert.Issuer.String()
}

// makeCert mints a certificate whose Issuer CN is issuerCN, expiring in validForDays.
func makeCert(t *testing.T, issuerCN string, validForDays int) types.CertificateList {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// The extra hour keeps daysUntil's truncating division landing on exactly
	// validForDays rather than one less once a few milliseconds have elapsed.
	notAfter := time.Now().Add(time.Duration(validForDays)*24*time.Hour + time.Hour)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "leaf"},
		Issuer:       pkix.Name{CommonName: issuerCN},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}

	// Self-signing would force Issuer == tmpl.Subject, so sign against a parent that
	// carries the issuer name we want.
	parent := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: issuerCN},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter.Add(24 * time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, key)
	require.NoError(t, err)

	return types.CertificateList{Data: der}
}

// certTestDevice is an Apple Silicon Mac: the population whose SCEP identity is meant to be
// replaced by ACME.
func certTestDevice() types.Device {
	return types.Device{UDID: certTestUDID, SerialNumber: certTestSerial, Model: "MacBookPro18,3"}
}

// intelTestDevice is an Intel Mac migrated from MicroMDM. It can never complete ACME, so its
// SCEP identity is the only one it will ever have.
func intelTestDevice() types.Device {
	return types.Device{UDID: certTestUDID, SerialNumber: certTestSerial, Model: "MacBookPro16,1"}
}

// unknownModelTestDevice has not reported DeviceInformation yet.
func unknownModelTestDevice() types.Device {
	return types.Device{UDID: certTestUDID, SerialNumber: certTestSerial}
}

// configureCertFlags points the flags at the issuers the fixtures actually used and wires
// re-enrollment at a webhook stub, returning a counter of how many times the enrollment
// profile was fetched. Whether that counter moves is the real observable: one fetch is one
// re-enrollment.
//
// The stub answers 500 so reinstallEnrollmentProfile stops there rather than going on to
// sign and push a profile.
func configureCertFlags(
	t *testing.T,
	certList []types.CertificateList,
) *int {
	t.Helper()
	registerCertFlags(t)

	fetches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	// Webhook mode clears the enrollment-profile-on-disk preflight, which would otherwise
	// return before the expiry decision is ever made.
	setFlag(t, "enable-reenroll-via-webhook", "true")
	setFlag(t, "enroll-webhook-url", server.URL)
	setFlag(t, "enroll-webhook-token", "test-token")
	setFlag(t, "scep-cert-min-validity", "10000")
	setFlag(t, "acme-cert-min-validity", "180")
	setFlag(t, "scep-cert-issuer", issuerString(t, findByIssuerCN(t, certList, certTestScepIssuerCN)))
	setFlag(t, "acme-cert-issuer", issuerString(t, findByIssuerCN(t, certList, certTestAcmeIssuerCN)))

	return &fetches
}

func findByIssuerCN(t *testing.T, certList []types.CertificateList, cn string) []byte {
	t.Helper()
	for _, item := range certList {
		cert, err := x509.ParseCertificate(item.Data)
		require.NoError(t, err)
		if cert.Issuer.CommonName == cn {
			return item.Data
		}
	}
	t.Fatalf("no fixture certificate issued by %q", cn)
	return nil
}

// scepBeforeAcmeCertList models the case this logic exists for: a device that already
// holds an ACME identity but still has its old SCEP certificate, with the SCEP one
// appearing FIRST and the ACME one last behind several unrelated certificates. A scan that
// stops at the first matching issuer never reaches the ACME identity.
//
// The unrelated entries are filler standing in for the assorted CA and vendor
// certificates a real device accumulates, including one long expired - none of them should
// influence the decision.
func scepBeforeAcmeCertList(t *testing.T, acmeDays int) []types.CertificateList {
	t.Helper()
	return []types.CertificateList{
		makeCert(t, "System Default CA", 7248),
		makeCert(t, "System Kerberos CA", 7248),
		makeCert(t, certTestScepIssuerCN, 1043),
		makeCert(t, "Unrelated Vendor Root CA", 819),
		makeCert(t, "Unrelated Corporate Root CA", 33118),
		makeCert(t, "Unrelated Agent CA", 1082),
		makeCert(t, "Unrelated Expired CA", -928),
		makeCert(t, "Unrelated Vendor CA", 5893),
		makeCert(t, certTestAcmeIssuerCN, acmeDays),
	}
}

// collectEnrollmentCerts is the part that decides; assert on it directly so the tests do
// not depend on reinstallEnrollmentProfile's transport.
func TestCollectEnrollmentCerts_FindsAcmeAfterScepInList(t *testing.T) {
	certList := scepBeforeAcmeCertList(t, 3646)
	configureCertFlags(t, certList)

	found := collectEnrollmentCerts(certList, certTestDevice())

	require.NotNil(t, found.scep, "SCEP certificate should be found where it appears first")
	require.NotNil(t, found.acme, "ACME identity must still be found past the SCEP match")
	assert.Equal(t, 3646, daysUntil(found.acme.NotAfter))
	assert.Equal(t, 1043, daysUntil(found.scep.NotAfter))
}

// The regression this fixes: a device with a healthy ACME identity and a stale SCEP
// certificate must not be re-enrolled, however high scep-cert-min-validity is set. Before
// the fix this fetched a fresh enrollment profile on every check-in, forever.
func TestValidateEnrollmentCertExpiry_HealthyAcmeWinsOverStaleScep(t *testing.T) {
	certList := scepBeforeAcmeCertList(t, 3646)
	fetches := configureCertFlags(t, certList)

	err := validateEnrollmentCertExpiry(certList, certTestDevice())

	assert.NoError(t, err, "a healthy ACME identity must suppress the SCEP check entirely")
	assert.Zero(t, *fetches, "no enrollment profile should be fetched")
}

// A device with no ACME identity must still be re-enrolled, so a high
// scep-cert-min-validity keeps working as a way to move devices off SCEP.
func TestValidateEnrollmentCertExpiry_ScepOnlyStillTriggers(t *testing.T) {
	certList := scepBeforeAcmeCertList(t, 3646)
	fetches := configureCertFlags(t, certList)

	// Drop the ACME identity, leaving the ordering otherwise intact.
	scepOnly := certList[:len(certList)-1]

	err := validateEnrollmentCertExpiry(scepOnly, certTestDevice())

	require.Error(t, err, "the webhook stub answers 500, so a fired trigger surfaces here")
	assert.Equal(t, 1, *fetches, "a SCEP-only device must still be re-enrolled")
}

// An expiring ACME identity is a genuine renewal and must trigger.
func TestValidateEnrollmentCertExpiry_ExpiringAcmeTriggers(t *testing.T) {
	certList := scepBeforeAcmeCertList(t, 30)
	fetches := configureCertFlags(t, certList)

	err := validateEnrollmentCertExpiry(certList, certTestDevice())

	require.Error(t, err)
	assert.Equal(t, 1, *fetches, "an ACME identity inside min-validity must be renewed")
}

// One malformed entry must not abandon the whole check.
func TestCollectEnrollmentCerts_SkipsUnparseableCert(t *testing.T) {
	certList := scepBeforeAcmeCertList(t, 3646)
	configureCertFlags(t, certList)

	withGarbage := append([]types.CertificateList{{Data: []byte("not-a-certificate")}}, certList...)

	found := collectEnrollmentCerts(withGarbage, certTestDevice())

	require.NotNil(t, found.acme, "parsing must continue past a malformed certificate")
	require.NotNil(t, found.scep)
}

// Longest-lived wins, so a stale duplicate cannot force a needless re-enrollment.
func TestCollectEnrollmentCerts_PrefersLongestLived(t *testing.T) {
	certList := scepBeforeAcmeCertList(t, 3646)
	configureCertFlags(t, certList)

	withStaleDupe := append([]types.CertificateList{makeCert(t, certTestAcmeIssuerCN, 10)}, certList...)

	found := collectEnrollmentCerts(withStaleDupe, certTestDevice())

	require.NotNil(t, found.acme)
	assert.Equal(t, 3646, daysUntil(found.acme.NotAfter), "the longer-lived identity should win")
}

// An Intel Mac holds only a SCEP identity and always will. However far inside
// scep-cert-min-validity that certificate is, it must not be re-enrolled: mdmenroll would hand
// back a profile the device cannot complete.
func TestValidateEnrollmentCertExpiry_ScepOnlyIntelNeverTriggers(t *testing.T) {
	certList := scepBeforeAcmeCertList(t, 3646)
	fetches := configureCertFlags(t, certList)
	scepOnly := certList[:len(certList)-1]

	err := validateEnrollmentCertExpiry(scepOnly, intelTestDevice())

	assert.NoError(t, err)
	assert.Zero(t, *fetches, "an Intel device must never be sent to mdmenroll")
}

// Until DeviceInformation has populated the model, the architecture is unknown. Treat that like
// Intel: for a Silicon Mac it only defers ACME renewal to the next info cycle, for an Intel Mac
// it is the only safe answer.
func TestValidateEnrollmentCertExpiry_ScepOnlyUnknownModelDoesNotTrigger(t *testing.T) {
	certList := scepBeforeAcmeCertList(t, 3646)
	fetches := configureCertFlags(t, certList)
	scepOnly := certList[:len(certList)-1]

	err := validateEnrollmentCertExpiry(scepOnly, unknownModelTestDevice())

	assert.NoError(t, err)
	assert.Zero(t, *fetches, "an unknown model must fail safe and not re-enroll")
}

// The architecture gate applies to the SCEP branch only: a Silicon Mac whose ACME identity is
// expiring is a genuine renewal even if, for whatever reason, its model is unreadable.
func TestValidateEnrollmentCertExpiry_ExpiringAcmeTriggersRegardlessOfModel(t *testing.T) {
	certList := scepBeforeAcmeCertList(t, 30)
	fetches := configureCertFlags(t, certList)

	err := validateEnrollmentCertExpiry(certList, unknownModelTestDevice())

	require.Error(t, err)
	assert.Equal(t, 1, *fetches, "an ACME identity proves the device can do ACME")
}
