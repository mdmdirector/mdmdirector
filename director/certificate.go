package director

import (
	"crypto/x509"
	"time"

	"github.com/grahamgilbert/mdmdirector/log"
	"github.com/grahamgilbert/mdmdirector/types"
	"github.com/pkg/errors"
)

func processCertificateList(certificateListData types.CertificateListData) error {
	for _, certListItem := range certificateListData.CertificateList {
		err := validateScepCert(certListItem)
		if err != nil {
			return errors.Wrap(err, "processCertificateList:validateScepCert")
		}

	}
	return nil
}

func validateScepCert(certListItem types.CertificateList) error {
	cert, err := x509.ParseCertificate(certListItem.Data)
	if err != nil {
		return errors.Wrap(err, "failed to parse certificate")
	}
	if cert.Issuer.CommonName == "MicroMDM" {
		log.Info(cert.NotAfter)
		end := time.Now().AddDate(0, 0, 30)
		if cert.NotAfter.Before(end) {
			log.Infof("Time is after %v for %v", end, cert.Issuer)
		}
	} else {
		log.Info("We would do some pushing on the enrollment profile here")
	}
	return nil
}
