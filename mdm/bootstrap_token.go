package mdm

import (
	"io"
	"net/http"
	"strings"

	"github.com/pkg/errors"
)

// ErrBootstrapTokenKeepUnknownDevice is returned when NanoMDM has no device with that UDID.
var ErrBootstrapTokenKeepUnknownDevice = errors.New("NanoMDM does not know this device")

// bootstrapTokenKeepEndpoint is served by Airbnb's NanoMDM build
// (ergo projects/nanomdm/internal/bstokenkeep), not by upstream NanoMDM.
const bootstrapTokenKeepEndpoint = "bootstrap-token-keep"

// KeepBootstrapToken tells NanoMDM to keep udid's escrowed Bootstrap Token when the
// device next sends Authenticate.
//
// NanoMDM clears the token on every Authenticate, but a Mac that replaces its
// enrollment profile in place (an identity renewal) sends Authenticate without
// sending SetBootstrapToken again, so the token would be lost
// (micromdm/nanomdm#302). Call this just before enqueuing that profile.
// NanoMDM expires the mark after 7 days if the Authenticate never comes.
func (c *NanoMDMClient) KeepBootstrapToken(udid string) error {
	return c.bootstrapTokenKeep(http.MethodPut, udid)
}

// ForgetBootstrapTokenKeep removes a mark set by KeepBootstrapToken, for a renewal
// that was not enqueued after all.
func (c *NanoMDMClient) ForgetBootstrapTokenKeep(udid string) error {
	return c.bootstrapTokenKeep(http.MethodDelete, udid)
}

func (c *NanoMDMClient) bootstrapTokenKeep(method, udid string) error {
	if udid == "" {
		return errors.New("bootstrapTokenKeep: empty UDID")
	}
	endpoint, err := c.buildURL(bootstrapTokenKeepEndpoint, []string{udid}, nil)
	if err != nil {
		return errors.Wrap(err, "bootstrapTokenKeep")
	}
	req, err := http.NewRequest(method, endpoint, nil)
	if err != nil {
		return errors.Wrap(err, "bootstrapTokenKeep: create request")
	}
	req.SetBasicAuth(NanoMDMAuthUsername, c.apiKey)

	resp, err := c.client.Do(req)
	var body []byte
	if resp != nil {
		defer resp.Body.Close()
		body, _ = io.ReadAll(io.LimitReader(resp.Body, 512))
	}
	if err != nil {
		return errors.Wrap(err, "bootstrapTokenKeep: execute request")
	}

	switch {
	case resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK:
		return nil
	// The endpoint answers 404 "unknown device"; a NanoMDM build without the endpoint
	// answers its router's plain 404, which must not be mistaken for that.
	case resp.StatusCode == http.StatusNotFound && strings.Contains(string(body), "unknown device"):
		return ErrBootstrapTokenKeepUnknownDevice
	case resp.StatusCode == http.StatusNotFound:
		return errors.New("bootstrapTokenKeep: endpoint not found (NanoMDM build without bootstrap-token-keep?)")
	default:
		return errors.Errorf("bootstrapTokenKeep: unexpected status %d", resp.StatusCode)
	}
}
