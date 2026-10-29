package director

import (
	"bytes"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/mdmdirector/mdmdirector/utils"
)

var migrationSyncClient = &http.Client{Timeout: 15 * time.Second}

// syncCheckinToMicroMDM mirrors a checkin event's raw plist to MicroMDM's migration-sync endpoint
func syncCheckinToMicroMDM(topic string, rawPayload []byte) {
	if !utils.DualWriteMicroMDM() {
		return
	}

	go func() {
		endpoint, err := url.Parse(utils.MicroMDMURL())
		if err != nil {
			ErrorLogger(LogHolder{Message: "migration dual-write to MicroMDM: " + err.Error(), Metric: topic})
			return
		}
		endpoint.Path = path.Join(endpoint.Path, "mdm", "migration-checkin")

		req, err := http.NewRequest(http.MethodPut, endpoint.String(), bytes.NewReader(rawPayload))
		if err != nil {
			ErrorLogger(LogHolder{Message: "migration dual-write to MicroMDM: " + err.Error(), Metric: topic})
			return
		}
		req.SetBasicAuth("micromdm", utils.MicroMDMAPIKey())

		resp, err := migrationSyncClient.Do(req)
		if err != nil {
			ErrorLogger(LogHolder{Message: "migration dual-write to MicroMDM failed: " + err.Error(), Metric: topic})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			ErrorLogger(LogHolder{Message: "migration dual-write to MicroMDM returned non-200", Metric: topic})
		}
	}()
}
