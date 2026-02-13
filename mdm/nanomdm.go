package mdm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
)

// ServerType represents MDM server type (microMDM or nanoMDM)
type ServerType string

const (
	ServerTypeMicroMDM ServerType = "micromdm"
	ServerTypeNanoMDM  ServerType = "nanomdm"
)

// NanoMDMAuthUsername - username used for Basic Auth with NanoMDM API
const NanoMDMAuthUsername = "nanomdm"

// ErrClientNotInitialized - when the NanoMDM client hasn't been initialized
var ErrClientNotInitialized = errors.New("NanoMDM client not initialized")

type MDMClient interface {
	Push(enrollmentIDs ...string) (*APIResponse, error)
	Enqueue(enrollmentIDs []string, payload types.CommandPayload, opts *EnqueueOptions) (*APIResponse, error)
	InspectQueue(enrollmentID string) (*QueueResponse, error)
	ClearQueue(enrollmentIDs ...string) (*QueueDeleteResponse, error)
	QueryEnrollments(filter *EnrollmentFilter, config *PaginationConfig) (*EnrollmentsResponse, error)
	GetAllEnrollments(config *PaginationConfig) (*EnrollmentsResponse, error)
}

type NanoMDMClient struct {
	serverURL string
	apiKey    string
	client    *utils.HTTPClient
}

// mdmClient holds the global MDM client instance
var mdmClient MDMClient

// InitClient initializes the global NanoMDM client.
func InitClient(serverURL, apiKey string) {
	mdmClient = &NanoMDMClient{
		serverURL: strings.TrimRight(serverURL, "/"),
		apiKey:    apiKey,
		client:    utils.NewHTTPClient(60*time.Second, nil),
	}
}

// SetClientForTesting allows injecting a mock client for testing
func SetClientForTesting(client MDMClient) {
	mdmClient = client
}

// Client returns the global NanoMDM client instance.
// Returns ErrClientNotInitialized if InitClient hasn't been called.
func Client() (MDMClient, error) {
	if mdmClient == nil {
		return nil, ErrClientNotInitialized
	}
	return mdmClient, nil
}

// buildURL constructs the API URL with path and optional query params
func (c *NanoMDMClient) buildURL(endpoint string, ids []string, queryParams map[string]string) (string, error) {
	u, err := url.Parse(c.serverURL)
	if err != nil {
		return "", errors.Wrap(err, "parse server URL")
	}

	// /v1/{endpoint}/{id1},{id2},...
	idPath := strings.Join(ids, ",")
	u.Path = path.Join(u.Path, "v1", endpoint, idPath)

	if len(queryParams) > 0 {
		q := u.Query()
		for k, v := range queryParams {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	return u.String(), nil
}

// doRequest performs an HTTP request and parses the APIResult response
func (c *NanoMDMClient) doRequest(req *http.Request) (*APIResponse, error) {
	req.SetBasicAuth(NanoMDMAuthUsername, c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "execute request")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read response body")
	}

	var result APIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, errors.Wrapf(err, "decode response: %s", string(body))
	}

	// 500 means all operations failed
	if resp.StatusCode == http.StatusInternalServerError {
		errMsg := result.PushError
		if result.CommandError != "" {
			errMsg = result.CommandError
		}
		if errMsg == "" {
			errMsg = "server error"
		}
		return &result, errors.New(errMsg)
	}

	return &result, nil
}
