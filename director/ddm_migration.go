package director

// -----------------------------------------------------------------------------
// TEMPORARY — MicroMDM -> NanoMDM migration only.
//
// This whole file (plus the `ddm_migration_optins` table) exists so we can flip
// individual devices (or cohorts) onto DDM independently of the global USE_DDM /
// USE_DDM_PACKAGES switches. That lets a migration wave land on NanoMDM in the
// classic InstallProfile / InstallApplication protocol first, and be promoted to
// DDM later, per device.
//
// A single opt-in row covers BOTH profiles and applications for a device. The two
// global flags remain independent enable-all master switches (see ddmForDevice /
// ddmPackagesForDevice below).
//
// ============================ CLEANUP CHECKLIST ==============================
// Once the whole fleet is on DDM and per-device control is no longer needed:
//   1. Delete this file.
//   2. Replace every call to ddmForDevice(d)        with utils.UseDDM().
//      Replace every call to ddmPackagesForDevice(d) with utils.UseDDMPackages().
//      Replace pushSharedProfilesPerDevice / deleteSharedProfilesPerDevice call
//        sites with the direct PushSharedProfiles / DeleteSharedProfiles calls
//        passing utils.UseDDM().
//      Restore the app functions in install_application.go to branch on
//        utils.UseDDMPackages() at the top (see the "migration" comments there).
//   3. Remove &DDMMigrationOptIn{} from db.DB.AutoMigrate(...) in main.go and drop
//      the two /device/{udid}/ddm-migrate routes.
//   4. DROP TABLE ddm_migration_optins;
// =============================================================================

import (
	"encoding/json"
	intErrors "errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
)

// DDMMigrationOptIn marks a single device as opted into DDM (for both profiles and
// applications) during the migration. Presence of a row == opted in.
type DDMMigrationOptIn struct {
	DeviceUDID string `gorm:"primaryKey" json:"device_udid"`
}

// TableName pins the table name so the CLEANUP "DROP TABLE" is unambiguous.
func (DDMMigrationOptIn) TableName() string { return "ddm_migration_optins" }

// deviceOptedIntoDDM reports whether a single device has been explicitly opted in.
func deviceOptedIntoDDM(udid string) bool {
	if udid == "" {
		return false
	}
	var count int64
	if err := db.DB.Model(&DDMMigrationOptIn{}).Where("device_udid = ?", udid).Count(&count).Error; err != nil {
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

// optedInSet loads, in a single query, the opt-in membership for the given devices
func optedInSet(devices []types.Device) map[string]bool {
	set := make(map[string]bool)
	if len(devices) == 0 {
		return set
	}
	udids := make([]string, 0, len(devices))
	for i := range devices {
		udids = append(udids, devices[i].UDID)
	}
	var optIns []DDMMigrationOptIn
	if err := db.DB.Where("device_udid IN (?)", udids).Find(&optIns).Error; err != nil {
		ErrorLogger(LogHolder{Message: "optedInSet: " + err.Error()})
		return set
	}
	for _, o := range optIns {
		set[o.DeviceUDID] = true
	}
	return set
}

// partitionByDDM splits devices into the DDM cohort and the classic cohort for PROFILE
func partitionByDDM(devices []types.Device) (ddmDevices, legacyDevices []types.Device) {
	global := utils.UseDDM()
	set := optedInSet(devices)
	for i := range devices {
		if global || set[devices[i].UDID] {
			ddmDevices = append(ddmDevices, devices[i])
		} else {
			legacyDevices = append(legacyDevices, devices[i])
		}
	}
	return
}

// partitionByDDMPackages splits devices into the DDM cohort and the classic cohort for APPLICATION
func partitionByDDMPackages(devices []types.Device) (ddmDevices, legacyDevices []types.Device) {
	global := utils.UseDDMPackages()
	set := optedInSet(devices)
	for i := range devices {
		if global || set[devices[i].UDID] {
			ddmDevices = append(ddmDevices, devices[i])
		} else {
			legacyDevices = append(legacyDevices, devices[i])
		}
	}
	return
}

// pushSharedProfilesPerDevice runs PushSharedProfiles split by each device's mode,
// so a fleet-wide push respects per-device DDM opt-in.
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

// deleteSharedProfilesPerDevice runs DeleteSharedProfiles split by each device's mode.
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

// teardownDDMForDevice best-effort removes a device's DDM PROFILE declarations/sets
// from KMFDDM when the device is demoted back to classic (Applications are not torn)
func teardownDDMForDevice(device types.Device) {
	var deviceProfiles []types.DeviceProfile
	if err := db.DB.Where("device_ud_id = ?", device.UDID).Find(&deviceProfiles).Error; err != nil {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, Message: "teardownDDMForDevice: load device profiles: " + err.Error()})
	} else if len(deviceProfiles) > 0 {
		if err := DeleteDeviceProfilesViaDDM([]types.Device{device}, deviceProfiles); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, Message: "teardownDDMForDevice: delete device profile declarations: " + err.Error()})
		}
	}

	var sharedProfiles []types.SharedProfile
	if err := db.DB.Find(&sharedProfiles).Error; err != nil {
		ErrorLogger(LogHolder{DeviceUDID: device.UDID, Message: "teardownDDMForDevice: load shared profiles: " + err.Error()})
	} else if len(sharedProfiles) > 0 {
		if err := DeleteSharedProfilesViaDDM([]types.Device{device}, sharedProfiles); err != nil {
			ErrorLogger(LogHolder{DeviceUDID: device.UDID, Message: "teardownDDMForDevice: delete shared profile declarations: " + err.Error()})
		}
	}
}

// DDMMigrateHandler (POST /device/{udid}/ddm-migrate) promotes a device to DDM by
// inserting an opt-in row, then reconciles it so its profiles/apps re-push via DDM.
func DDMMigrateHandler(w http.ResponseWriter, r *http.Request) {
	udid := mux.Vars(r)["udid"]
	device, err := GetDevice(udid)
	if err != nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}

	optIn := DDMMigrationOptIn{DeviceUDID: udid}
	if err := db.DB.Where(&optIn).FirstOrCreate(&optIn).Error; err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "DDMMigrateHandler: create opt-in: " + err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	InfoLogger(LogHolder{DeviceUDID: udid, DeviceSerial: device.SerialNumber, Message: "DDM migration: device promoted to DDM"})

	// Reconcile now so declarations get created and the device switches protocol
	if _, err := InstallAllProfiles(device); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "DDMMigrateHandler: InstallAllProfiles: " + err.Error()})
	}
	// Install applications via DDM declarations
	if _, err := InstallBootstrapPackages(device); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "DDMMigrateHandler: InstallBootstrapPackages: " + err.Error()})
	}

	writeDDMMigrateStatus(w, udid, true)
}

// DDMUnmigrateHandler (DELETE /device/{udid}/ddm-migrate) demotes a device back to
// the classic protocol: removes the opt-in row, tears down its DDM declarations,
// and reconciles so profiles/apps re-push via InstallProfile
func DDMUnmigrateHandler(w http.ResponseWriter, r *http.Request) {
	udid := mux.Vars(r)["udid"]
	device, err := GetDevice(udid)
	if err != nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}

	// Tear down DDM state while the device is still considered "DDM", then remove
	// the opt-in row so subsequent operations use the classic path.
	teardownDDMForDevice(device)

	if err := db.DB.Where("device_udid = ?", udid).Delete(&DDMMigrationOptIn{}).Error; err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "DDMUnmigrateHandler: delete opt-in: " + err.Error()})
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	InfoLogger(LogHolder{DeviceUDID: udid, DeviceSerial: device.SerialNumber, Message: "DDM migration: device demoted to classic InstallProfile"})

	// Reconcile profiles now so they re-push via the classic path. Applications are
	// intentionally left as-is: anything installed via a DDM declaration stays installed
	if _, err := InstallAllProfiles(device); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "DDMUnmigrateHandler: InstallAllProfiles: " + err.Error()})
	}

	writeDDMMigrateStatus(w, udid, false)
}

func writeDDMMigrateStatus(w http.ResponseWriter, udid string, useDDM bool) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"device_udid": udid,
		"use_ddm":     useDDM,
	}); err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: "writeDDMMigrateStatus: " + err.Error()})
	}
}
