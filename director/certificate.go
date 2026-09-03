package director

import (
	"crypto/x509"
	"fmt"
	"strconv"
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func RequestCertificateList(device types.Device) error {
	requestType := "CertificateList"
	DebugLogger(LogHolder{Message: "Requesting Certificate List", DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, CommandRequestType: requestType})
	var payload types.CommandPayload
	payload.UDID = device.UDID
	payload.RequestType = requestType
	_, err := SendCommand(payload)
	if err != nil {
		return errors.Wrap(err, "RequestCertificateList: SendCommand")
	}

	return nil
}

func processCertificateList(certificateListData types.CertificateListData, device types.Device) error {
	var certificates []types.Certificate
	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Saving CertificateList"})

	for _, certListItem := range certificateListData.CertificateList {
		var certificate types.Certificate
		cert, err := parseCertificate(certListItem)
		if err != nil {
			log.Errorf("processCertificateList:parseCertificate: %v", err)
			continue
		}

		certificate.Data = certListItem.Data
		certificate.CommonName = cert.Issuer.CommonName
		certificate.NotAfter = cert.NotAfter
		certificate.NotBefore = cert.NotBefore
		certificate.Subject = cert.Subject.String()
		certificate.Issuer = cert.Issuer.String()
		certificates = append(certificates, certificate)
	}

	err := db.DB.Model(&device).Association("Certificates").Replace(certificates)
	if err != nil {
		return errors.Wrap(err, "processCertificateList:SaveCerts")
	}

	err = validateEnrollmentCertExpiry(certificateListData.CertificateList, device)
	if err != nil {
		return errors.Wrap(err, "processCertificateList:validateEnrollmentCertExpiry")
	}

	return nil
}

func parseCertificate(certListItem types.CertificateList) (*x509.Certificate, error) {
	cert, err := x509.ParseCertificate(certListItem.Data)
	if err != nil {
		return nil, errors.Wrap(err, "parseCertificate:failed to parse certificate")
	}
	return cert, nil
}

// enrollmentCerts holds the two certificates that matter to the enrollment decision,
// picked out of a device's full CertificateList
type enrollmentCerts struct {
	// acme is the certificate issued by acme-cert-issuer
	acme *x509.Certificate
	// scep is the certificate issued by scep-cert-issuer
	scep *x509.Certificate
}

// daysUntil returns whole days from now until t, negative once t has passed.
func daysUntil(t time.Time) int {
	return int(time.Until(t).Hours() / 24)
}

// collectEnrollmentCerts scans a device's whole CertificateList for its ACME identity and
// its legacy SCEP identity
func collectEnrollmentCerts(certList []types.CertificateList, device types.Device) enrollmentCerts {
	scepIssuer := utils.ScepCertIssuer()
	acmeIssuer := utils.AcmeCertIssuer()

	var found enrollmentCerts

	for _, certListItem := range certList {
		cert, err := parseCertificate(certListItem)
		if err != nil {
			DebugLogger(LogHolder{
				DeviceSerial: device.SerialNumber,
				DeviceUDID:   device.UDID,
				Message:      fmt.Sprintf("Skipping unparseable certificate: %v", err),
			})
			continue
		}

		issuer := cert.Issuer.String()
		DebugLogger(LogHolder{
			DeviceSerial: device.SerialNumber,
			DeviceUDID:   device.UDID,
			Message:      fmt.Sprintf("Certificate issued by %s issuer", issuer),
			Metric:       strconv.Itoa(daysUntil(cert.NotAfter)),
		})

		switch {
		case acmeIssuer != "" && issuer == acmeIssuer:
			if found.acme == nil || cert.NotAfter.After(found.acme.NotAfter) {
				found.acme = cert
			}
		case issuer == scepIssuer:
			if found.scep == nil || cert.NotAfter.After(found.scep.NotAfter) {
				found.scep = cert
			}
		}
	}

	return found
}

// validateEnrollmentCertExpiry triggers re-enrollment when the certificate that actually
// manages the device is nearing expiry
//
// The ACME identity takes precedence: once a device holds one it has migrated, and only
// that certificate's expiry matters. The legacy SCEP certificate is consulted only when
// the device has no ACME identity
func validateEnrollmentCertExpiry(certList []types.CertificateList, device types.Device) error {
	if !utils.EnableReEnrollViaWebhook() {
		enrollmentProfile := utils.EnrollmentProfile()
		if enrollmentProfile == "" {
			InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "No enrollment profile set, not continuing with enrollment cert expiry check"})
			return nil
		}

		if !utils.FileExists(enrollmentProfile) {
			return errors.New("Enrollment profile isn't present at path")
		}
	}

	found := collectEnrollmentCerts(certList, device)

	var (
		cert        *x509.Certificate
		minValidity int
		kind        string
	)

	switch {
	case found.acme != nil:
		cert, minValidity, kind = found.acme, utils.AcmeCertMinValidity(), "ACME"
	case found.scep != nil:
		// intel devices are not allowed to re-enroll
		if !canReEnrollViaACME(device) {
			arch := deviceArchitecture(device)
			InfoLogger(LogHolder{
				DeviceSerial: device.SerialNumber,
				DeviceUDID:   device.UDID,
				Message:      fmt.Sprintf("SCEP enrollment certificate on a %s device (model %q); ACME re-enrollment is not possible, leaving enrollment alone", arch, device.Model),
				Metric:       strconv.Itoa(daysUntil(found.scep.NotAfter)),
			})
			metrics.ReenrollSkipped("cert_expiry", string(arch)).Inc()
			return nil
		}
		cert, minValidity, kind = found.scep, utils.ScepCertMinValidity(), "SCEP"
	default:
		// Neither enrollment certificate is on the device; nothing to renew.
		return nil
	}

	days := daysUntil(cert.NotAfter)
	if days > minValidity {
		DebugLogger(LogHolder{
			DeviceSerial: device.SerialNumber,
			DeviceUDID:   device.UDID,
			Message:      fmt.Sprintf("%s enrollment certificate is valid, not re-enrolling", kind),
			Metric:       strconv.Itoa(days),
		})
		return nil
	}

	InfoLogger(LogHolder{
		DeviceSerial: device.SerialNumber,
		DeviceUDID:   device.UDID,
		Message:      fmt.Sprintf("Certificate issued by %s issuer", cert.Issuer.String()),
		Metric:       strconv.Itoa(days),
	})

	return reinstallEnrollmentProfile(device)
}
