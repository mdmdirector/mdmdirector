package director

// DDM enabled status
//
// Reports whether a device has actually turned on the declarative management engine,
// as opposed to whether mdmdirector merely sent the command. The truth comes from
// KMFDDM: once a device turns the engine on it reports values under the
// .StatusItems.management tree on the DDM status channel (client-capabilities is present
// even when no declarations are installed). If KMFDDM holds any such value for the
// enrollment, DDM is enabled on the device.

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/ddm"
)

// ddmManagementStatusPrefix is the SQL LIKE prefix that matches any value the device
// reported under the DDM management status tree.
const ddmManagementStatusPrefix = ".StatusItems.management.%"

// DeviceDDMStatusHandler (GET /device/{udid}/ddm/status) reports whether the device has
// the declarative management engine turned on, per KMFDDM's status reports.
func DeviceDDMStatusHandler(w http.ResponseWriter, r *http.Request) {
	client, err := ddm.Client()
	if err != nil {
		ErrorLogger(LogHolder{Message: "DeviceDDMStatusHandler: " + err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	udid := mux.Vars(r)["udid"]
	if _, err := GetDevice(udid); err != nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}

	values, err := client.GetStatusValues(udid, ddmManagementStatusPrefix)
	if err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "DeviceDDMStatusHandler: " + err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	enabled := len(values) > 0

	body := map[string]interface{}{
		"device_udid":  udid,
		"ddm_enabled":  enabled,
		"status_count": len(values),
	}
	if last := latestStatusTimestamp(values); last != "" {
		body["last_status_at"] = last
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "DeviceDDMStatusHandler: encode: " + err.Error()})
	}
}

// latestStatusTimestamp returns the newest RFC3339 timestamp among the values, or "" when
// there are none. KMFDDM emits timestamps in a lexicographically sortable format, so a
// string max is the newest.
func latestStatusTimestamp(values []ddm.StatusValue) string {
	latest := ""
	for i := range values {
		if values[i].Timestamp > latest {
			latest = values[i].Timestamp
		}
	}
	return latest
}
