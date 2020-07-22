package director

import (
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/groob/plist"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func RequestCertificateList(device types.Device) {
	requestType := "CertificateList"
	DebugLogger(LogHolder{Message: "Requesting Certificate List", DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, CommandRequestType: requestType})
	var payload types.CommandPayload
	payload.UDID = device.UDID
	payload.RequestType = requestType
	_, err := SendCommand(payload)
	if err != nil {
		log.Error(errors.Wrap(err, "RequestCertificateList: SendCommand"))
	}
}

func processCertificateList(certificateListData types.CertificateListData, device types.Device) error {
	var certificates []types.Certificate
	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Saving CertificateList"})

	for _, certListItem := range certificateListData.CertificateList {
		var certificate types.Certificate
		cert, err := parseCertificate(certListItem)
		if err != nil {
			log.Errorf("processCertificateList:parseCertificate: %v", err)
		}

		certificate.Data = certListItem.Data
		certificate.CommonName = cert.Issuer.CommonName
		certificate.NotAfter = cert.NotAfter
		certificate.Subject = cert.Subject.String()
		certificates = append(certificates, certificate)
		err = validateScepCert(certListItem, device)
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

func validateScepCert(certListItem types.CertificateList, device types.Device) error {
	enrollmentProfile := utils.EnrollmentProfile()
	if enrollmentProfile == "" {
		// No enrollment profile set
		return nil
	}

	if !utils.FileExists(enrollmentProfile) {
		err := errors.New("Enrollment profile isn't present at path")
		return err
	}
	cert, err := parseCertificate(certListItem)
	if err != nil {
		return errors.Wrap(err, "failed to parse certificate")
	}
	if cert.Issuer.CommonName == utils.ScepCertIssuer() {
		end := time.Now().AddDate(0, 0, -utils.ScepCertMinValidity())
		if cert.NotAfter.After(end) {
			errMsg := fmt.Sprintf("Certificate issued by %v is expiring before %v.", utils.ScepCertIssuer(), end)
			InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: errMsg})

			data, err := ioutil.ReadFile(enrollmentProfile)
			if err != nil {
				return errors.Wrap(err, "failed to read enrollment profile")
			}

			var profile types.DeviceProfile

			err = plist.Unmarshal(data, &profile)
			if err != nil {
				return errors.Wrap(err, "Failed to unmarshal enrollment profile to struct")
			}

			profile.MobileconfigData = data

			_, err = PushProfiles([]types.Device{device}, []types.DeviceProfile{profile})
			if err != nil {
				return errors.Wrap(err, "Failed to push enrollment profile")
			}
		}
	}
	return nil
}
