package director

// Per-device DDM control
//
// mdmdirector can manage profiles and applications via Declarative Device Management
// (DDM). The global USE_DDM / USE_DDM_PACKAGES flags enable DDM for the entire fleet.
// This file adds finer-grained control: an opt-in list that enables DDM for individual
// devices while the global flags remain off.

import (
	"encoding/json"
	intErrors "errors"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
)

// DDMOptIn marks a single device as opted into DDM (for both profiles and
// applications). Presence of a row == opted in.
type DDMOptIn struct {
	DeviceUDID string `gorm:"primaryKey" json:"device_udid"`
}

// TableName sets an explicit table name for the opt-in list
func (DDMOptIn) TableName() string { return "ddm_opt_ins" }

// deviceOptedIntoDDM reports whether a single device has been explicitly opted in
func deviceOptedIntoDDM(udid string) bool {
	if udid == "" {
		return false
	}
	var count int64
	if err := db.DB.Model(&DDMOptIn{}).Where("device_udid = ?", udid).Count(&count).Error; err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "deviceOptedIntoDDM: " + err.Error()})
		return false
	}
	return count > 0
}

// ddmForDevice decides whether PROFILE operations for this device use DDM
func ddmForDevice(device types.Device) bool {
	return utils.UseDDM() || deviceOptedIntoDDM(device.UDID)
}

// ddmPackagesForDevice decides whether APPLICATION operations for this device use DDM
func ddmPackagesForDevice(device types.Device) bool {
	return utils.UseDDMPackages() || deviceOptedIntoDDM(device.UDID)
}

// optedInSet loads the opt-in membership for the given devices
func optedInSet(devices []types.Device) map[string]bool {
	set := make(map[string]bool)
	if len(devices) == 0 {
		return set
	}
	udids := make([]string, 0, len(devices))
	for i := range devices {
		udids = append(udids, devices[i].UDID)
	}
	var optIns []DDMOptIn
	if err := db.DB.Where("device_udid IN (?)", udids).Find(&optIns).Error; err != nil {
		ErrorLogger(LogHolder{Message: "optedInSet: " + err.Error()})
		return set
	}
	for _, o := range optIns {
		set[o.DeviceUDID] = true
	}
	return set
}

// partitionByGlobalOrOptIn splits devices into the DDM cohort and the non-DDM cohort.
func partitionByGlobalOrOptIn(devices []types.Device, global bool) (ddmDevices, legacyDevices []types.Device) {
	if global {
		return devices, nil
	}
	set := optedInSet(devices)
	for i := range devices {
		if set[devices[i].UDID] {
			ddmDevices = append(ddmDevices, devices[i])
		} else {
			legacyDevices = append(legacyDevices, devices[i])
		}
	}
	return
}

// partitionByDDM splits devices into the DDM cohort and the non-DDM cohort for PROFILE operations
func partitionByDDM(devices []types.Device) (ddmDevices, legacyDevices []types.Device) {
	return partitionByGlobalOrOptIn(devices, utils.UseDDM())
}

// partitionByDDMPackages splits devices into the DDM cohort and the non-DDM cohort for APPLICATION operations
func partitionByDDMPackages(devices []types.Device) (ddmDevices, legacyDevices []types.Device) {
	return partitionByGlobalOrOptIn(devices, utils.UseDDMPackages())
}

// pushSharedProfilesPerDevice runs PushSharedProfiles split by each device's mode,
// so a fleet-wide push respects per-device DDM opt-in
func pushSharedProfilesPerDevice(devices []types.Device, profiles []types.SharedProfile) error {
	ddmDevices, legacyDevices := partitionByDDM(devices)
	var errs []error
	if len(ddmDevices) > 0 {
		if _, err := PushSharedProfiles(ddmDevices, profiles, true); err != nil {
			errs = append(errs, err)
		}
	}
	if len(legacyDevices) > 0 {
		if _, err := PushSharedProfiles(legacyDevices, profiles, false); err != nil {
			errs = append(errs, err)
		}
	}
	return intErrors.Join(errs...)
}

// deleteSharedProfilesPerDevice runs DeleteSharedProfiles split by each device's mode
func deleteSharedProfilesPerDevice(devices []types.Device, profiles []types.SharedProfile) error {
	ddmDevices, legacyDevices := partitionByDDM(devices)
	var errs []error
	if len(ddmDevices) > 0 {
		if _, err := DeleteSharedProfiles(ddmDevices, profiles, true); err != nil {
			errs = append(errs, err)
		}
	}
	if len(legacyDevices) > 0 {
		if _, err := DeleteSharedProfiles(legacyDevices, profiles, false); err != nil {
			errs = append(errs, err)
		}
	}
	return intErrors.Join(errs...)
}

// teardownDDMForDevice removes a device's DDM PROFILE declarations/sets
// from KMFDDM when DDM is disabled for the device
//
// Applications are intentionally left in place: an app already installed via a DDM
// declaration can stay installed
func teardownDDMForDevice(device types.Device) error {
	var errs []error
	fail := func(stage string, err error) {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, Message: "teardownDDMForDevice: " + stage + ": " + err.Error()})
		errs = append(errs, fmt.Errorf("%s: %w", stage, err))
	}

	var deviceProfiles []types.DeviceProfile
	if err := db.DB.Where("device_ud_id = ?", device.UDID).Find(&deviceProfiles).Error; err != nil {
		fail("load device profiles", err)
	} else if len(deviceProfiles) > 0 {
		if err := DeleteDeviceProfilesViaDDM([]types.Device{device}, deviceProfiles); err != nil {
			fail("delete device profile declarations", err)
		}
	}

	var sharedProfiles []types.SharedProfile
	if err := db.DB.Find(&sharedProfiles).Error; err != nil {
		fail("load shared profiles", err)
	} else if len(sharedProfiles) > 0 {
		if err := DeleteSharedProfilesViaDDM([]types.Device{device}, sharedProfiles); err != nil {
			fail("delete shared profile declarations", err)
		}
	}

	return intErrors.Join(errs...)
}

// EnableDeviceDDMHandler (POST /device/{udid}/ddm) opts a device into DDM by inserting
// an opt-in row, then reconciles it so its profiles and applications install via DDM
func EnableDeviceDDMHandler(w http.ResponseWriter, r *http.Request) {
	udid := mux.Vars(r)["udid"]
	device, err := GetDevice(udid)
	if err != nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}

	optIn := DDMOptIn{DeviceUDID: udid}
	if err := db.DB.Where(&optIn).FirstOrCreate(&optIn).Error; err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "EnableDeviceDDMHandler: create opt-in: " + err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	InfoLogger(LogHolder{DeviceUDID: udid, DeviceSerial: device.SerialNumber, Message: "DDM enabled for device"})

	var reconcileErrs []error

	// Reconcile now so declarations get created and the device switches to DDM.
	if _, err := InstallAllProfiles(device); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "EnableDeviceDDMHandler: InstallAllProfiles: " + err.Error()})
		reconcileErrs = append(reconcileErrs, fmt.Errorf("InstallAllProfiles: %w", err))
	}
	// Install applications via DDM declarations.
	if _, err := InstallBootstrapPackages(device); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "EnableDeviceDDMHandler: InstallBootstrapPackages: " + err.Error()})
		reconcileErrs = append(reconcileErrs, fmt.Errorf("InstallBootstrapPackages: %w", err))
	}

	writeDeviceDDMStatus(w, udid, true, intErrors.Join(reconcileErrs...))
}

// DisableDeviceDDMHandler (DELETE /device/{udid}/ddm) opts a device out of DDM: tears
// down its DDM profile declarations, removes the opt-in row, and re-pushes profiles via
// InstallProfile commands
func DisableDeviceDDMHandler(w http.ResponseWriter, r *http.Request) {
	udid := mux.Vars(r)["udid"]
	device, err := GetDevice(udid)
	if err != nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}

	var reconcileErrs []error

	// Tear down DDM state while the device is still opted in
	if err := teardownDDMForDevice(device); err != nil {
		reconcileErrs = append(reconcileErrs, fmt.Errorf("teardownDDMForDevice: %w", err))
	}

	if err := db.DB.Where("device_udid = ?", udid).Delete(&DDMOptIn{}).Error; err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "DisableDeviceDDMHandler: delete opt-in: " + err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	InfoLogger(LogHolder{DeviceUDID: udid, DeviceSerial: device.SerialNumber, Message: "DDM disabled for device"})

	// Re-push profiles via InstallProfile commands. Applications are intentionally left as is
	if _, err := InstallAllProfiles(device); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "DisableDeviceDDMHandler: InstallAllProfiles: " + err.Error()})
		reconcileErrs = append(reconcileErrs, fmt.Errorf("InstallAllProfiles: %w", err))
	}

	writeDeviceDDMStatus(w, udid, false, intErrors.Join(reconcileErrs...))
}

// writeDeviceDDMStatus reports the device's mode along with whether the follow-up
// reconcile fully succeeded. The mode change itself is already committed, so a failed
// reconcile is a partial success (207) rather than an error: the opt-in state stands and
// the device catches up on the next reconcile
func writeDeviceDDMStatus(w http.ResponseWriter, udid string, useDDM bool, reconcileErr error) {
	body := map[string]interface{}{
		"device_udid": udid,
		"use_ddm":     useDDM,
		"reconciled":  reconcileErr == nil,
	}

	status := http.StatusOK
	if reconcileErr != nil {
		status = http.StatusMultiStatus
		body["reconcile_errors"] = errorMessages(reconcileErr)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "writeDeviceDDMStatus: " + err.Error()})
	}
}

// errorMessages flattens a joined error into one message per underlying failure
func errorMessages(err error) []string {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		msgs := make([]string, 0, len(joined.Unwrap()))
		for _, e := range joined.Unwrap() {
			msgs = append(msgs, e.Error())
		}
		return msgs
	}
	return []string{err.Error()}
}
