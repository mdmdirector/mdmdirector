package mdm

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeepBootstrapToken(t *testing.T) {
	const udid = "DFAD4D66-8218-509D-94FF-DB8DEA25BCF6"

	keep := func(c *NanoMDMClient) error { return c.KeepBootstrapToken(udid) }
	cases := []struct {
		name    string
		call    func(c *NanoMDMClient) error
		status  int
		body    string
		method  string
		wantErr error
		anyErr  bool
	}{
		{"keep 204", keep, http.StatusNoContent, "", http.MethodPut, nil, false},
		{"keep unknown device", keep, http.StatusNotFound, "unknown device\n", http.MethodPut, ErrBootstrapTokenKeepUnknownDevice, true},
		{"keep endpoint missing (old NanoMDM build)", keep, http.StatusNotFound, "404 page not found\n", http.MethodPut, nil, true},
		{"keep 400", keep, http.StatusBadRequest, "bad ttl", http.MethodPut, nil, true},
		{"forget 204", func(c *NanoMDMClient) error { return c.ForgetBootstrapTokenKeep(udid) }, http.StatusNoContent, "", http.MethodDelete, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod, gotPath, gotUser, gotPass string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				gotUser, gotPass, _ = r.BasicAuth()
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			err := tc.call(NewClient(srv.URL, "secret", WithMaxRetries(0)))
			if tc.anyErr {
				require.Error(t, err)
				if tc.wantErr != nil {
					assert.ErrorIs(t, err, tc.wantErr)
				} else {
					assert.NotErrorIs(t, err, ErrBootstrapTokenKeepUnknownDevice)
				}
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.method, gotMethod)
			assert.Equal(t, "/v1/bootstrap-token-keep/"+udid, gotPath)
			assert.Equal(t, NanoMDMAuthUsername, gotUser)
			assert.Equal(t, "secret", gotPass)
		})
	}

	t.Run("empty udid", func(t *testing.T) {
		require.Error(t, NewClient("http://unused", "k").KeepBootstrapToken(""))
	})
}
