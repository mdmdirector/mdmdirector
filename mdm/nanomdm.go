package mdm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

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

// NanoMDMClient is a client for the NanoMDM API
type NanoMDMClient struct {
	serverURL string
	apiKey    string
	client    *utils.HTTPClient
}

// nanoMDMClient holds the global NanoMDM client instance
var nanoMDMClient *NanoMDMClient

// ClientOption configures a NanoMDMClient
type ClientOption func(*NanoMDMClient)

// WithMaxRetries sets the maximum number of HTTP retries
func WithMaxRetries(n int) ClientOption {
	return func(c *NanoMDMClient) {
		cfg := utils.DefaultRetryConfig()
		cfg.MaxRetries = n
		c.client = utils.NewHTTPClient(60*time.Second, &cfg)
	}
}

// NewClient creates a new NanoMDMClient
func NewClient(serverURL, apiKey string, opts ...ClientOption) *NanoMDMClient {
	c := &NanoMDMClient{
		serverURL: strings.TrimRight(serverURL, "/"),
		apiKey:    apiKey,
		client:    utils.NewHTTPClient(60*time.Second, nil),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// InitClient initializes the global NanoMDM client.
func InitClient(serverURL, apiKey string) {
	nanoMDMClient = NewClient(serverURL, apiKey)
}

// Client returns the global NanoMDM client instance
func Client() (*NanoMDMClient, error) {
	if nanoMDMClient == nil {
		return nil, ErrClientNotInitialized
	}
	return nanoMDMClient, nil
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
