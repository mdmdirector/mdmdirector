package director

import (
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"strconv"
	"time"

	"github.com/groob/plist"
	"github.com/mdmdirector/mdmdirector/db"
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
		}

		certificate.Data = certListItem.Data
		certificate.CommonName = cert.Issuer.CommonName
		certificate.NotAfter = cert.NotAfter
		certificate.NotBefore = cert.NotBefore
		certificate.Subject = cert.Subject.String()
		certificate.Issuer = cert.Issuer.String()
		certificates = append(certificates, certificate)
	}

	// DebugLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: certificates})

	err := db.DB.Model(&device).Association("Certificates").Replace(certificates)
	if err != nil {
		return errors.Wrap(err, "processCertificateList:SaveCerts")
	}

	for _, certListItem := range certificateListData.CertificateList {
		scepErr := validateScepCert(certListItem, device)
		if scepErr != nil {
			return errors.Wrap(scepErr, "processCertificateList:validateScepCert")
		}
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

func validateScepCert(certListItem types.CertificateList, device types.Device) error {
	enrollmentProfile := utils.EnrollmentProfile()
	if enrollmentProfile == "" {
		InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "No emrollment profile set, not continuing with SCEP Cert Validation"})
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

	if cert.Issuer.String() == utils.ScepCertIssuer() {
		days := int(time.Until(cert.NotAfter).Hours() / 24)
		errMsg := fmt.Sprintf("Certificate issued by %v.", utils.ScepCertIssuer())
		DebugLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: errMsg, Metric: strconv.Itoa(days)})
		if days <= utils.ScepCertMinValidity() {
			InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: errMsg, Metric: strconv.Itoa(days)})

			data, err := ioutil.ReadFile(enrollmentProfile)
			if err != nil {
				return errors.Wrap(err, "Failed to read enrollment profile")
			}

			var profile types.DeviceProfile

			err = plist.Unmarshal(data, &profile)
			if err != nil {
				return errors.Wrap(err, "Failed to unmarshal enrollment profile to struct")
			}

			profile.MobileconfigData = data

			InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Pushing new enrollment profile"})

			if utils.SignedEnrollmentProfile() {
				DebugLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Enrollment Profile pre-signed"})
				var commandPayload types.CommandPayload
				commandPayload.RequestType = "InstallProfile"
				commandPayload.Payload = base64.StdEncoding.EncodeToString(profile.MobileconfigData)
				commandPayload.UDID = device.UDID

				_, err := SendCommand(commandPayload)
				if err != nil {
					return errors.Wrap(err, "Failed to push enrollment profile")
				}
			} else {
				DebugLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Signing Enrollment Profile"})
				_, err = PushProfiles([]types.Device{device}, []types.DeviceProfile{profile})
				if err != nil {
					return errors.Wrap(err, "Failed to push enrollment profile")
				}
			}

		} else {
			InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: "Days remaining is greater or equal than the minimum SCEP validity", Metric: strconv.Itoa(days)})
		}
	} else {
		msg := fmt.Sprintf("Incoming cert issuer %v does not match our SCEP issuer %v", cert.Issuer.String(), utils.ScepCertIssuer())
		InfoLogger(LogHolder{DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID, Message: msg})
	}
	return nil
}
