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
// A single device is activated over HTTP (ActivateDeviceDDMHandler). The whole fleet is
// activated at startup via the activate-ddm-fleet flag (see ActivateDDMFleet), not over
// the API.

import (
	"encoding/json"
	intErrors "errors"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/types"
)

// activateDDM sends a bare DeclarativeManagement command to a single enrollment.
// It attaches no declarations and writes no opt-in state.
func activateDDM(client *ddm.KMFDDMClient, udid string) error {
	// Associate the enrollment with its per-device set (noNotify=true) so KMFDDM knows
	// the enrollment exists. The set may hold no declarations - that is fine, nothing is
	// converted.
	if err := client.PutEnrollmentSet(udid, udid, true); err != nil {
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

// activateDevices activates DDM for each device in the slice, skipping empty UDIDs. It
// returns the count activated and a joined error of any per-device failures. A single
// failure does not abort the rest.
func activateDevices(client *ddm.KMFDDMClient, devices []types.Device) (activated int, err error) {
	var errs []error
	for i := range devices {
		device := devices[i]
		if device.UDID == "" {
			continue
		}
		if aerr := activateDDM(client, device.UDID); aerr != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: aerr.Error()})
			errs = append(errs, aerr)
			continue
		}
		activated++
		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "DDM activated for device"})
	}
	return activated, intErrors.Join(errs...)
}

// ActivateDDMFleet sends the DeclarativeManagement command to every device. It returns
// the number of devices activated and a joined error of any per-device failures. This is
// invoked at startup when the activate-ddm-fleet flag is set, not over the API.
func ActivateDDMFleet() (activated int, err error) {
	client, err := ddm.Client()
	if err != nil {
		return 0, err
	}

	devices, err := GetAllDevices()
	if err != nil {
		return 0, fmt.Errorf("ActivateDDMFleet: get all devices: %w", err)
	}

	return activateDevices(client, devices)
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
