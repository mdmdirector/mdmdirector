package ddm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// KMFDDMAuthUsername - username for basic auth with KMFDDM
const KMFDDMAuthUsername = "kmfddm"

// ErrClientNotInitialized - when the KMFDDM client hasn't been initialized
var ErrClientNotInitialized = fmt.Errorf("KMFDDM Client not initialized")

// kmfddmClient - global client instance
var kmfddmClient *KMFDDMClient

// InitClient initializes the global KMFDDM client
func InitClient(baseURL, apiKey string) {
	kmfddmClient = NewKMFDDMClient(baseURL, apiKey)
}

// Client returns the global KMFDDM client instance
func Client() (*KMFDDMClient, error) {
	if kmfddmClient == nil {
		return nil, ErrClientNotInitialized
	}
	return kmfddmClient, nil
}

// KMFDDMClient is a client for the KMFDDM API
type KMFDDMClient struct {
	baseURL    string
	username   string
	apiKey     string
	httpClient *http.Client
}

// NewKMFDDMClient creates a new KMFDDM API client.
func NewKMFDDMClient(baseURL, apiKey string) *KMFDDMClient {
	return &KMFDDMClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		username:   KMFDDMAuthUsername,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// doRequest executes an HTTP request against the KMFDDM API
func (c *KMFDDMClient) doRequest(method, urlPath string, queryParams url.Values, body []byte) (*http.Response, error) {
	endpoint, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, errors.Wrap(err, "parsing KMFDDM base URL")
	}
	endpoint.Path = path.Join(endpoint.Path, urlPath)

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	if queryParams != nil {
		endpoint.RawQuery = queryParams.Encode()
	}

	req, err := http.NewRequest(method, endpoint.String(), bodyReader)
	if err != nil {
		return nil, errors.Wrap(err, "creating KMFDDM request")
	}

	req.SetBasicAuth(c.username, c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "KMFDDM %s %s", method, urlPath)
	}

	return resp, nil
}

// PutDeclaration stores a declaration in KMFDDM (creating or overwriting)
// Returns changed=true when the declaration was new or modified (HTTP 204),
// changed=false when unchanged (HTTP 304)
func (c *KMFDDMClient) PutDeclaration(declaration Declaration, noNotify bool) (changed bool, err error) {
	body, err := json.Marshal(declaration)
	if err != nil {
		return false, errors.Wrap(err, "marshaling declaration")
	}

	params := url.Values{}
	if noNotify {
		params.Set("nonotify", "true")
	}

	resp, err := c.doRequest("PUT", "/v1/declarations", params, body)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent: // 204 = changed/new in KMFDDM
		return true, nil
	case http.StatusNotModified: // 304 = unchanged in KMFDDM
		return false, nil
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("KMFDDM PUT /v1/declarations returned unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
}

// TouchDeclaration bumps the ServerToken of an existing declaration without changing its content
// Devices will re-fetch and re-apply the declaration - used to force profile reinstallation
func (c *KMFDDMClient) TouchDeclaration(declarationID string, noNotify bool) error {
	params := url.Values{}
	if noNotify {
		params.Set("nonotify", "true")
	}

	urlPath := fmt.Sprintf("/v1/declarations/%s/touch", declarationID)
	resp, err := c.doRequest("POST", urlPath, params, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent: // 204 = success
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("KMFDDM declaration %q not found for touch", declarationID)
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("KMFDDM POST %s returned unexpected status %d: %s", urlPath, resp.StatusCode, string(respBody))
	}
}

// PutSetDeclaration associates a declaration with a set
func (c *KMFDDMClient) PutSetDeclaration(setName, declarationID string, noNotify bool) error {
	params := url.Values{}
	params.Set("declaration", declarationID)
	if noNotify {
		params.Set("nonotify", "true")
	}

	urlPath := fmt.Sprintf("/v1/set-declarations/%s", setName)
	resp, err := c.doRequest("PUT", urlPath, params, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusNotModified: // 204 = changed, 304 = unchanged - both OK
		return nil
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("KMFDDM PUT %s returned unexpected status %d: %s", urlPath, resp.StatusCode, string(respBody))
	}
}

// DeleteDeclaration removes a declaration from KMFDDM
func (c *KMFDDMClient) DeleteDeclaration(declarationID string, noNotify bool) error {
	params := url.Values{}
	if noNotify {
		params.Set("nonotify", "true")
	}

	urlPath := fmt.Sprintf("/v1/declarations/%s", declarationID)
	resp, err := c.doRequest("DELETE", urlPath, params, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusNotModified: // both OK
		return nil
	case http.StatusNotFound:
		// Declaration already gone - not an error for deletion
		return nil
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("KMFDDM DELETE %s returned unexpected status %d: %s", urlPath, resp.StatusCode, string(respBody))
	}
}

// DeleteSetDeclaration removes the association between a declaration and a set
func (c *KMFDDMClient) DeleteSetDeclaration(setName, declarationID string, noNotify bool) error {
	params := url.Values{}
	params.Set("declaration", declarationID)
	if noNotify {
		params.Set("nonotify", "true")
	}

	urlPath := fmt.Sprintf("/v1/set-declarations/%s", setName)
	resp, err := c.doRequest("DELETE", urlPath, params, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusNotModified: // both OK
		return nil
	case http.StatusNotFound:
		// Association already gone - not an error for deletion
		return nil
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("KMFDDM DELETE %s returned unexpected status %d: %s", urlPath, resp.StatusCode, string(respBody))
	}
}

// NotifyEnrollment triggers a DDM sync for an enrollment ID via POST /v1/notify.
// Unlike PutEnrollmentSet with nonotify=false, this bypasses the changed-check entirely.
func (c *KMFDDMClient) NotifyEnrollment(udid string) error {
	params := url.Values{}
	params.Set("id", udid)

	resp, err := c.doRequest("POST", "/v1/notify", params, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("KMFDDM POST /v1/notify returned unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
}

// StatusValue is one status value an enrollment reported on the DDM status channel, as
// returned by KMFDDM's GET /v1/status-values. Value is left raw because KMFDDM reports
// strings, numbers and booleans in the same field.
type StatusValue struct {
	Path      string          `json:"path"`
	Value     json.RawMessage `json:"value"`
	Timestamp string          `json:"timestamp"`
	StatusID  string          `json:"status_id"`
}

// GetStatusValues retrieves the status values an enrollment reported, optionally filtered
// by a SQL LIKE path prefix (use '%' as the wildcard). KMFDDM keys the response by
// enrollment ID; this returns just the slice for the requested enrollment (nil if the
// enrollment has reported nothing under the prefix).
func (c *KMFDDMClient) GetStatusValues(enrollmentID, pathPrefix string) ([]StatusValue, error) {
	params := url.Values{}
	if pathPrefix != "" {
		params.Set("prefix", pathPrefix)
	}

	resp, err := c.doRequest("GET", "/v1/status-values/"+enrollmentID, params, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("KMFDDM GET /v1/status-values/%s returned unexpected status %d: %s", enrollmentID, resp.StatusCode, string(respBody))
	}

	// Response shape: { "<enrollmentID>": [ {path,value,timestamp,status_id}, ... ] }
	var byEnrollment map[string][]StatusValue
	if err := json.NewDecoder(resp.Body).Decode(&byEnrollment); err != nil {
		return nil, errors.Wrap(err, "decoding KMFDDM status values")
	}

	return byEnrollment[enrollmentID], nil
}

// PutEnrollmentSet associates an enrollment ID with a set
func (c *KMFDDMClient) PutEnrollmentSet(enrollmentID, setName string, noNotify bool) error {
	params := url.Values{}
	params.Set("set", setName)
	if noNotify {
		params.Set("nonotify", "true")
	}

	urlPath := fmt.Sprintf("/v1/enrollment-sets/%s", enrollmentID)
	resp, err := c.doRequest("PUT", urlPath, params, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusNotModified: // 204 = changed, 304 = unchanged - both OK
		return nil
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("KMFDDM PUT %s returned unexpected status %d: %s", urlPath, resp.StatusCode, string(respBody))
	}
}
