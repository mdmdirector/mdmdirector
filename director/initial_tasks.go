package director

import (
	"fmt"
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

// initialTasksLeaseDuration is the maximum time RunInitialTasks is presumed to be running
// before another caller may take the lease. It is the single source of truth for the lease
// window: the in-process check (initialTasksInFlight) uses it directly and the SQL lease
// UPDATE uses initialTasksLeaseInterval, which is derived from it.
const initialTasksLeaseDuration = 5 * time.Minute

// initialTasksLeaseInterval is initialTasksLeaseDuration rendered as a PostgreSQL interval
// literal (e.g. "300 seconds") for tryAcquireInitialTasksLease.
var initialTasksLeaseInterval = fmt.Sprintf("%d seconds", int64(initialTasksLeaseDuration/time.Second))

// initialTasksInFlight reports whether a RunInitialTasks invocation for this device is
// presumed to still be running: the lease start time is set and younger than the TTL.
// It is a read of the already-loaded device row, so callers can skip the lease
// UPDATE (and the lease_contention count it produces) for the many device events -
// mostly command acknowledgements - that arrive while the run is still in progress.
// A stale start time (older than the TTL) returns false so the normal lease path can
// take over from a holder that died mid-run.
func initialTasksInFlight(device *types.Device) bool {
	if device == nil || device.RunInitialTasksStarttime == nil {
		return false
	}
	return time.Since(*device.RunInitialTasksStarttime) < initialTasksLeaseDuration
}

// tryAcquireInitialTasksLease attempts to atomically claim the RunInitialTasks lease for this UDID
// Returns true if the caller now owns the lease, false if another invocation holds it
func tryAcquireInitialTasksLease(udid string) (bool, error) {
	res := db.DB.Exec(`
		UPDATE devices
		SET    run_initial_tasks_starttime = NOW()
		WHERE  ud_id = ?
		  AND  initial_tasks_run = false
		  AND  ( run_initial_tasks_starttime IS NULL
				 OR run_initial_tasks_starttime < NOW() - INTERVAL '`+initialTasksLeaseInterval+`' )
	`, udid)
	if res.Error != nil {
		return false, errors.Wrap(res.Error, "tryAcquireInitialTasksLease")
	}
	return res.RowsAffected == 1, nil
}

// releaseInitialTasksLease clears the lease so a retry isn't blocked for the full TTL
func releaseInitialTasksLease(udid string) {
	err := db.DB.Exec(`
		UPDATE devices
		SET    run_initial_tasks_starttime = NULL
		WHERE  ud_id = ?
		  AND  initial_tasks_run = false
	`, udid).Error
	if err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: errors.Wrap(err, "releaseInitialTasksLease").Error()})
	}
}

func RunInitialTasks(udid string) (retErr error) {
	if udid == "" {
		err := errors.New("No Device UDID")
		if utils.Prometheus() {
			metrics.InitialTasks("error").Inc()
		}
		return errors.Wrap(err, "RunInitialTasks")
	}

	acquired, err := tryAcquireInitialTasksLease(udid)
	if err != nil {
		if utils.Prometheus() {
			metrics.InitialTasks("error").Inc()
		}
		return errors.Wrap(err, "RunInitialTasks")
	}
	if !acquired {
		InfoLogger(LogHolder{DeviceUDID: udid, Message: "RunInitialTasks lease not acquired - already running or already complete; skipping"})
		if utils.Prometheus() {
			metrics.InitialTasks("lease_contention").Inc()
		}
		return nil
	}

	completed := false
	defer func() {
		if !completed {
			releaseInitialTasksLease(udid)
		}
		if utils.Prometheus() {
			metrics.InitialTasks(metrics.ResultFromError(retErr)).Inc()
		}
	}()

	device, err := GetDevice(udid)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks")
	}
	InfoLogger(LogHolder{Message: "Running initial tasks", DeviceSerial: device.SerialNumber, DeviceUDID: device.UDID})
	err = ClearCommands(&device)
	if err != nil {
		return err
	}

	err = RequestAllDeviceInfo(device)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks:RequestAllDeviceInfo")
	}

	_, err = InstallAllProfiles(device)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks:InstallAllProfiles")
	}

	_, err = InstallBootstrapPackages(device)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks:InstallBootstrapPackages")
	}
	err = processDeviceConfigured(device)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks:processDeviceConfigured")
	}

	// processDeviceConfigured -> SaveDeviceConfigured already set initial_tasks_run = true
	completed = true
	return nil
}

func processDeviceConfigured(device types.Device) error {
	var deviceModel types.Device
	err := SendDeviceConfigured(device)
	if err != nil {
		return errors.Wrap(err, "RunInitialTasks")
	}
	err = SaveDeviceConfigured(device)
	if err != nil {
		return err
	}
	err = db.DB.Model(&deviceModel).Select("last_info_requested").Where("ud_id = ?", device.UDID).Updates(map[string]interface{}{"last_info_requested": time.Now()}).Error
	if err != nil {
		return err
	}

	return nil
}

func SendDeviceConfigured(device types.Device) error {
	requestType := "DeviceConfigured"
	var commandPayload types.CommandPayload
	commandPayload.UDID = device.UDID
	commandPayload.RequestType = requestType
	_, err := SendCommand(commandPayload)
	if err != nil {
		return errors.Wrap(err, "SendDeviceConfigured")
	}
	// Twice for luck
	_, err = SendCommand(commandPayload)
	if err != nil {
		return errors.Wrap(err, "SendDeviceConfigured")
	}
	return nil
}

func SaveDeviceConfigured(device types.Device) error {
	var deviceModel types.Device
	now := time.Now()
	err := db.DB.Model(&deviceModel).Select("token_update_recieved", "authenticate_recieved", "initial_tasks_run", "last_checked_in", "next_push").Where("ud_id = ?", device.UDID).Updates(map[string]interface{}{"token_update_recieved": true, "authenticate_recieved": true, "initial_tasks_run": true, "last_checked_in": now, "next_push": now}).Error
	if err != nil {
		return err
	}

	return nil
}

func ResetDevice(device types.Device) error {
	var deviceModel types.Device
	err := ClearCommands(&device)
	if err != nil {
		return errors.Wrap(err, "ResetDevice:ClearCommands")
	}
	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Resetting device"})
	err = db.DB.Model(&deviceModel).Where("ud_id = ?", device.UDID).Updates(map[string]interface{}{
		"token_update_recieved":       false,
		"authenticate_recieved":       false,
		"initial_tasks_run":           false,
		"active":                      false,
		"run_initial_tasks_starttime": gorm.Expr("NULL"),
	}).Error
	if err != nil {
		return errors.Wrap(err, "reset device")
	}
	return nil
}
