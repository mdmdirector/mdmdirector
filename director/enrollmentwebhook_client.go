package director

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"net/http"
	"time"

	"github.com/fullsailor/pkcs7"
	"github.com/groob/plist"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
)

// machineInfoPlist represents the MachineInfo plist
type machineInfoPlist struct {
	IMEI                       string `plist:"IMEI,omitempty"`
	MEID                       string `plist:"MEID,omitempty"`
	OSVersion                  string `plist:"OS_VERSION,omitempty"`
	Product                    string `plist:"PRODUCT"`
	Serial                     string `plist:"SERIAL"`
	SupplementalBuildVersion   string `plist:"SUPPLEMENTAL_BUILD_VERSION,omitempty"`
	SupplementalOSVersionExtra string `plist:"SUPPLEMENTAL_OS_VERSION_EXTRA,omitempty"`
	UDID                       string `plist:"UDID"`
	Version                    string `plist:"VERSION,omitempty"`
}

// buildMachineInfoHeader constructs machineInfo plist from device's attributes
func buildMachineInfoHeader(device types.Device) (string, error) {
	info := machineInfoPlist{
		IMEI:                       device.IMEI,
		MEID:                       device.MEID,
		OSVersion:                  device.OSVersion,
		Product:                    device.Model,
		Serial:                     device.SerialNumber,
		SupplementalBuildVersion:   device.SupplementalBuildVersion,
		SupplementalOSVersionExtra: device.SupplementalOSVersionExtra,
		UDID:                       device.UDID,
		Version:                    device.BuildVersion,
	}

	plistBytes, err := plist.Marshal(info)
	if err != nil {
		return "", errors.Wrap(err, "marshal MachineInfo plist")
	}

	// X-Apple-Aspen-Deviceinfo is an Apple-format PKCS7/CMS-signed plist; real
	// devices always send it wrapped this way. We match that format so any
	// enrollment endpoint expecting the standard header can parse it.
	wrapped, err := wrapPKCS7(plistBytes)
	if err != nil {
		return "", errors.Wrap(err, "wrap MachineInfo plist in PKCS7")
	}

	return base64.StdEncoding.EncodeToString(wrapped), nil
}

// wrapPKCS7 wraps data in a PKCS7 signed-data structure using an ephemeral self-signed certificate.
// The signature is not intended to be verified by the receiver; it exists only so the payload is valid PKCS7 DER
func wrapPKCS7(data []byte) ([]byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, errors.Wrap(err, "generate signing key")
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "mdmdirector-machineinfo"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, errors.Wrap(err, "create signing certificate")
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, errors.Wrap(err, "parse signing certificate")
	}

	sd, err := pkcs7.NewSignedData(data)
	if err != nil {
		return nil, errors.Wrap(err, "new signed data")
	}

	if err := sd.AddSigner(cert, priv, pkcs7.SignerInfoConfig{}); err != nil {
		return nil, errors.Wrap(err, "add signer")
	}

	signed, err := sd.Finish()
	if err != nil {
		return nil, errors.Wrap(err, "finish signed data")
	}

	return signed, nil
}

// fetchEnrollmentProfileFromWebhook gets enrollment profile from the enrollment profile webhook endpoint
func fetchEnrollmentProfileFromWebhook(device types.Device) ([]byte, error) {
	url := utils.EnrollWebhookURL()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "create enrollment profile webhook request")
	}

	req.Header.Set("Authorization", "Bearer "+utils.EnrollWebhookToken())

	headerValue, err := buildMachineInfoHeader(device)
	if err != nil {
		return nil, errors.Wrap(err, "build MachineInfo header")
	}
	req.Header.Set("X-Apple-Aspen-Deviceinfo", headerValue)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "enrollment profile webhook request failed")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read enrollment profile webhook response body")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("enrollment profile webhook returned status %d: %s", resp.StatusCode, string(body))
	}

	if len(body) == 0 {
		return nil, errors.New("enrollment profile webhook returned empty enrollment profile")
	}

	InfoLogger(LogHolder{
		DeviceUDID:   device.UDID,
		DeviceSerial: device.SerialNumber,
		Message:      "Successfully fetched enrollment profile from webhook",
	})
	return body, nil
}
