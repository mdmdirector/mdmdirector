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

// ErrClientNotInitialized - when the NanoMDM client hasn't been initialized
var ErrClientNotInitialized = errors.New("NanoMDM client not initialized")

type NanoMDMClient struct {
	serverURL string
	apiKey    string
	client    *utils.HTTPClient
}

var nanoClient *NanoMDMClient

// InitClient initializes the global NanoMDM client.
func InitClient(serverURL, apiKey string) {
	nanoClient = &NanoMDMClient{
		serverURL: strings.TrimRight(serverURL, "/"),
		apiKey:    apiKey,
		client:    utils.NewHTTPClient(60*time.Second, nil),
	}
}

// Client returns the global NanoMDM client instance.
// Returns ErrClientNotInitialized if InitClient hasn't been called.
func Client() (*NanoMDMClient, error) {
	if nanoClient == nil {
		return nil, ErrClientNotInitialized
	}
	return nanoClient, nil
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
	req.SetBasicAuth("nanomdm", c.apiKey)

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
