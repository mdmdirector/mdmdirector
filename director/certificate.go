package director

import (
	"crypto/x509"
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/log"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/pkg/errors"
)

func RequestCertificateList(device types.Device) {
	requestType := "CertificateList"
	log.Debugf("Requesting Certificate List for %v", device.UDID)
	var payload types.CommandPayload
	payload.UDID = device.UDID
	payload.RequestType = requestType
	SendCommand(payload)
}

func processCertificateList(certificateListData types.CertificateListData, device types.Device) error {
	var certificates []types.Certificate
	log.Infof("Saving Certificate List for %v", device.UDID)

	for _, certListItem := range certificateListData.CertificateList {
		var certificate types.Certificate
		cert, err := parseCertificate(certListItem)
		if err != nil {
			log.Errorf("processCertificateList:parseVertificate: %v", err)
		}

		certificate.Data = certListItem.Data
		certificate.CommonName = cert.Issuer.CommonName
		certificate.NotAfter = cert.NotAfter
		certificate.Subject = cert.Subject.String()
		certificates = append(certificates, certificate)
		err = validateScepCert(certListItem)
		if err != nil {
			return errors.Wrap(err, "processCertificateList:validateScepCert")
		}

	}

	err := db.DB.Unscoped().Model(&device).Association("Certificates").Replace(certificates).Error
	if err != nil {
		log.Error(err)
	}
	return nil
}

func parseCertificate(certListItem types.CertificateList) (*x509.Certificate, error) {
	cert, err := x509.ParseCertificate(certListItem.Data)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse certificate")
	}
	return cert, nil
}

func validateScepCert(certListItem types.CertificateList) error {
	cert, err := parseCertificate(certListItem)
	if err != nil {
		return errors.Wrap(err, "failed to parse certificate")
	}
	if cert.Issuer.CommonName == "MicroMDM" {
		log.Info(cert.NotAfter)
		end := time.Now().AddDate(0, 0, 30)
		if cert.NotAfter.Before(end) {
			log.Infof("Time is after %v for %v", end, cert.Issuer)
		} else {
			log.Info("We would do some pushing on the enrollment profile here")
		}
	}
	return nil
}
