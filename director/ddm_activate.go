package director

// Bare DDM activation
//
// This sends only the DeclarativeManagement command to a device so it enters
// declarative mode and can use DDM as needed. It does NOT convert any profiles or
// applications into declarations and it persists no opt-in state: unlike
// EnableDeviceDDMHandler, future profile/app pushes still follow whatever the global
// flags / per-device opt-in decide.
//
// The device-facing DeclarativeManagement command is emitted by KMFDDM in response to
// NotifyEnrollment. We first associate the enrollment with its (possibly empty)
// per-device set so KMFDDM knows the enrollment, then notify to force the command.
//
// A single device is activated over HTTP (ActivateDeviceDDMHandler).

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/utils"
)

// activateDDM sends a bare DeclarativeManagement command to a single enrollment.
// It attaches no declarations and writes no opt-in state.
func activateDDM(client *ddm.KMFDDMClient, udid string) error {
	// Associate the enrollment with its per-device set (noNotify=true) so KMFDDM knows
	// the enrollment exists. The set may hold no declarations - that is fine, nothing is
	// converted.
	if err := client.PutEnrollmentSet(udid, ddm.DeviceSetName(utils.DDMSetPrefix(), udid), true); err != nil {
		return fmt.Errorf("activateDDM: PUT enrollment-set for %s: %w", udid, err)
	}

	// Notify to trigger the DDM sync - bypasses the changed-check so the device always
	// receives a DeclarativeManagement command regardless of prior enrollment state.
	err := client.NotifyEnrollment(udid)
	observeDDMNotify(err)
	if err != nil {
		return fmt.Errorf("activateDDM: notify enrollment for %s: %w", udid, err)
	}

	return nil
}

// ActivateDeviceDDMHandler (POST /device/{udid}/ddm/activate) sends a bare
// DeclarativeManagement command so the device enters declarative mode without converting
// any profiles or applications.
func ActivateDeviceDDMHandler(w http.ResponseWriter, r *http.Request) {
	client, err := ddm.Client()
	if err != nil {
		ErrorLogger(LogHolder{Message: "ActivateDeviceDDMHandler: " + err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	udid := mux.Vars(r)["udid"]
	device, err := GetDevice(udid)
	if err != nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}
	if initialTasksPending(device, deferPathDDMActivate) {
		http.Error(w, "device is enrolling; initial tasks pending, retry later", http.StatusConflict)
		return
	}

	if err := activateDDM(client, udid); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "ActivateDeviceDDMHandler: " + err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	InfoLogger(LogHolder{DeviceUDID: udid, DeviceSerial: device.SerialNumber, Message: "DDM activated for device"})

	body := map[string]interface{}{
		"device_udid": udid,
		"activated":   true,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "ActivateDeviceDDMHandler: encode: " + err.Error()})
	}
}
