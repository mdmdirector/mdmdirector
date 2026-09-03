package director

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/bsm/redislock"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/vmihailenco/taskq/v3"
)

// pushTask is registered once at package load. taskq.RegisterTask panics on a
// duplicate name, so it must not live inside ScheduledCheckin (which may be called
// more than once, e.g. in tests).
var pushTask = taskq.RegisterTask(&taskq.TaskOptions{
	Name: "push",
	Handler: func(uuid string) error {
		err := PushDevice(uuid)
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
		}
		return nil
	},
})

// ScheduledCheckin runs the control plane: a periodic, fleet-wide scan that enqueues
// due devices for push and performs DB cleanup. It runs once immediately and then every
// `interval`. Each run is gated by a shared Redis lock (rc), so at most one replica runs
// the scan per tick; the rest skip. This is what lets mdmdirector scale horizontally --
// the data plane (the taskq consumer) scales with pod count while this control plane
// stays single-flight with automatic failover, and no leader election.
//
// `interval` is the scan cadence (env CONTROL_PLANE_INTERVAL); it is NOT the per-device
// push cadence, which stays bounded by `onceIn` (ONCE_IN) via the OnceInPeriod dedup.
func ScheduledCheckin(ctx context.Context, rc redislock.RedisClient, pushQueue taskq.Queue, onceIn, interval time.Duration) {
	task := pushTask

	run := func() {
		if ctx.Err() != nil {
			return
		}
		ran, err := withControlPlaneLock(ctx, rc, func(context.Context) error {
			log.Info("Running scheduled checkin")
			// Refresh the device inventory once per interval, fleet-wide, under the
			// control-plane lock -- instead of once per replica at startup. Only the
			// lock holder fetches, then scans and enqueues with fresh data.
			FetchDevicesFromMDM()
			return processScheduledCheckin(pushQueue, task, onceIn)
		})
		if err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			return
		}
		if !ran {
			DebugLogger(LogHolder{Message: "Skipping scheduled checkin; another replica holds the control-plane lock"})
		}
	}

	// Run once immediately, then every interval, until shutdown.
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func ProcessScheduledCheckinQueue(ctx context.Context, pushQueue taskq.Queue) {
	p := pushQueue.Consumer()
	DebugLogger(LogHolder{Message: "Processing items from scheduled checkin Queue"})
	err := p.Start(ctx)
	if err != nil {
		msg := fmt.Errorf("starting consumer: %v", err.Error())
		ErrorLogger(LogHolder{Message: msg.Error()})
		return
	}

	// Start is non-blocking; keep this goroutine alive until shutdown, then
	// drain in-flight work gracefully.
	<-ctx.Done()
	if err := p.Stop(); err != nil {
		ErrorLogger(LogHolder{Message: fmt.Errorf("stopping consumer: %v", err.Error()).Error()})
	}
}

func processScheduledCheckin(pushQueue taskq.Queue, task *taskq.Task, onceIn time.Duration) error {
	if utils.DebugMode() {
		DebugLogger(LogHolder{Message: "Processing scheduledCheckin in debug mode"})
	}

	err := pushAll(pushQueue, task, onceIn)
	if err != nil {
		return errors.Wrap(err, "processScheduledCheckin::pushAll")
	}

	return runCleanup()
}

// runCleanup performs the periodic DB maintenance mutations: removing orphaned
// certificate/profile-list rows, expiring random unlock PINs older than 30 minutes, and
// clearing fixed unlock PINs on devices that are no longer being erased/locked. It runs
// under the control-plane lock, so exactly one replica performs these mutations per
// interval instead of every replica racing on the same rows.
func runCleanup() error {
	var certificates []types.Certificate

	err := db.DB.Unscoped().Model(&certificates).Where("device_ud_id is NULL").Delete(&types.Certificate{}).Error
	if err != nil {
		return errors.Wrap(err, "runCleanup::CleanupNullCertificates")
	}

	var profileLists []types.ProfileList

	err = db.DB.Unscoped().Model(&profileLists).Where("device_ud_id is NULL").Delete(&types.ProfileList{}).Error
	if err != nil {
		return errors.Wrap(err, "runCleanup::CleanupNullProfileLists")
	}

	thirtyMinsAgo := time.Now().Add(-30 * time.Minute)
	err = db.DB.Where("unlock_pins.pin_set < ?", thirtyMinsAgo).Delete(&types.UnlockPin{}).Error
	if err != nil {
		return errors.Wrap(err, "runCleanup::DeleteRandomUnlockPins")
	}

	var device types.Device
	err = db.DB.Model(&device).Not("unlock_pin = ?", "").Where("erase = ? AND lock = ?", false, false).Update("unlock_pin", "").Error
	if err != nil {
		return errors.Wrap(err, "runCleanup::ResetFixedPin")
	}

	return nil
}

func deviceChunkSlice(slice []types.Device, chunkSize int) [][]types.Device {
	var chunks [][]types.Device
	for i := 0; i < len(slice); i += chunkSize {
		end := i + chunkSize

		// necessary check to avoid slicing beyond
		// slice capacity
		if end > len(slice) {
			end = len(slice)
		}

		chunks = append(chunks, slice[i:end])
	}

	return chunks
}

func pushAll(pushQueue taskq.Queue, task *taskq.Task, onceIn time.Duration) error {
	var devices []types.Device
	var dbDevices []types.Device

	DelaySeconds := getDelay() // nolint:staticcheck

	// Exclude user-channel enrollments (see fetchDevicesFromNanoMDM). They are not
	// devices, so the device-channel info commands never come back for them.
	err := db.DB.Where("ud_id NOT LIKE ?", "%:%").Find(&dbDevices).Scan(&dbDevices).Error
	if err != nil {
		return errors.Wrap(err, "PushAll: Scan devices")
	}

	if len(dbDevices) == 0 {
		InfoLogger(LogHolder{Message: "No enrolled devices, skipping push scheduling"})
		return nil
	}

	for i := range dbDevices {
		device := dbDevices[i]
		needsPush := deviceNeedsPush(device)

		if needsPush {
			InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Adding Device to push list"})
			devices = append(devices, device)
		}

	}

	DebugLogger(LogHolder{
		Message: "Pushing to all in debug mode",
	})

	counter := 0
	total := 0
	devicesPerSecond := float64(len(devices)) / float64((DelaySeconds - 1))
	DebugLogger(LogHolder{Message: "Processed devices per 0.5 seconds", Metric: strconv.Itoa(int(devicesPerSecond))})

	devicesPerMinute := int(math.Ceil(float64(len(devices)) / 60))
	InfoLogger(LogHolder{Message: fmt.Sprintf("%d will be processed each minute", devicesPerMinute)})
	deviceChunks := deviceChunkSlice(devices, devicesPerMinute)
	InfoLogger(LogHolder{Message: fmt.Sprintf("%d chunks of %d devices each will be processed", len(deviceChunks), devicesPerMinute)})
	ctx := context.Background()
	msgTxt := fmt.Sprintf("commands will only be queued for an individual device every %s at maximum", onceIn)
	InfoLogger(LogHolder{Message: msgTxt})
	for i := range deviceChunks {
		for j := range deviceChunks[i] {
			device := deviceChunks[i][j]
			if float64(counter) >= devicesPerSecond {
				DebugLogger(LogHolder{Message: "Sleeping due to having processed devices", Metric: strconv.Itoa(total)})
				time.Sleep(500 * time.Millisecond)
				counter = 0
			}
			DebugLogger(LogHolder{Message: "pushAll processed", Metric: strconv.Itoa(counter)})

			msg := task.WithArgs(ctx, device.UDID)

			msg.OnceInPeriod(onceIn)
			err := pushQueue.Add(msg)
			switch {
			case errors.Is(msg.Err, taskq.ErrDuplicate):
				// handle duplicate task
				DebugLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: msg.Err.Error()})
			case err != nil:
				ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: err.Error()})
			case msg.Err != nil:
				// handle duplicate task
				ErrorLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: msg.Err.Error()})
			}

			counter++
			total++
		}
		// Wait 1 minute before processing the next chunk of devices
		msg := fmt.Sprintf("%d/%d devices processed", (i+1)*devicesPerMinute, len(devices))
		InfoLogger(LogHolder{Message: msg})
		InfoLogger(LogHolder{Message: "Sleeping 1 minute before processing next chunk of devices"})
		time.Sleep(time.Minute * 1)
	}
	InfoLogger(LogHolder{Message: "Completed scheduling pushes", Metric: strconv.Itoa(len(devices))})
	return nil
}

func deviceNeedsPush(device types.Device) bool {
	now := time.Now()
	oneDayAgo := time.Now().Add(-24 * time.Hour)

	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Considering device for scheduled push"})

	if now.Before(device.NextPush) && !device.NextPush.IsZero() {
		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Not Pushing. Next push is in metric", Metric: device.NextPush.String()})
		return false
	}

	if device.LastCertificateList.IsZero() || device.LastProfileList.IsZero() || device.LastSecurityInfo.IsZero() || device.LastDeviceInfo.IsZero() {
		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "One or more of the info commands hasn't ever been received"})
		return true
	}

	// We've not had all of the info payloads within the last day
	if (device.LastCertificateList.Before(oneDayAgo) || device.LastProfileList.Before(oneDayAgo) || device.LastSecurityInfo.Before(oneDayAgo) || device.LastDeviceInfo.Before(oneDayAgo)) && (!device.LastCertificateList.IsZero() && !device.LastProfileList.IsZero() && !device.LastSecurityInfo.IsZero() && !device.LastDeviceInfo.IsZero()) {
		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Have not received all of the info commands within the last six hours."})
		return true
	}

	// If it's been updated within the last three hours, try to push again as it might still be online
	// if device.LastCheckedIn.After(threeHoursAgo) {
	// 	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Checked in more than three hours ago"})
	// 	if now.Before(device.NextPush) {
	// 		InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Not Pushing. Next push is in metric", Metric: device.NextPush.String()})
	// 		return false
	// 	}
	// }

	return true
}

func PushDevice(udid string) (err error) {
	if utils.Prometheus() {
		defer func() {
			metrics.PushRequests(metrics.ResultFromError(err)).Inc()
		}()
	}

	err = pushDevice(udid)
	if err == nil {
		// Record when this device is next eligible for a scheduled push. The
		// control-plane scan (deviceNeedsPush) skips devices whose NextPush is still in
		// the future, so this bounds per-device push cadence at ONCE_IN and stops
		// pushAll re-enqueuing recently pushed devices on every scan.
		updateNextPush(udid)
	}
	return err
}

// updateNextPush persists a device's next eligible scheduled-push time (now + ONCE_IN).
// Best-effort: a failure is logged but does not fail the push that already succeeded.
func updateNextPush(udid string) {
	next := time.Now().Add(time.Minute * time.Duration(utils.OnceIn()))
	if err := db.DB.Model(&types.Device{}).Where("ud_id = ?", udid).Update("next_push", next).Error; err != nil {
		ErrorLogger(LogHolder{DeviceUDID: udid, Message: errors.Wrap(err, "updateNextPush").Error()})
	}
}

func pushDevice(udid string) error {
	// Use NanoMDM client if enabled
	if utils.MDMServerType() == string(mdm.ServerTypeNanoMDM) {
		nanoClient, err := mdm.Client()
		if err != nil {
			return err
		}
		return pushDeviceWithClient(nanoClient, udid)
	}

	// MicroMDM implementation
	device := types.Device{UDID: udid}
	InfoLogger(LogHolder{DeviceUDID: device.UDID, Message: "Sending push to device"})
	now := time.Now()
	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Performing device push"})

	// This will ensure the apns push is expired when we push again - this helps prevent a flood of responses from the device whilst ensuring there is always a push waiting for the device when it comes online.
	retry := now.Add(time.Minute * time.Duration(int64(utils.OnceIn())))
	retryUnix := retry.Unix()

	endpoint, err := url.Parse(utils.MicroMDMURL())
	if err != nil {
		return errors.Wrap(err, "PushDevice")
	}

	endpoint.Path = path.Join(endpoint.Path, "push", device.UDID)
	queryString := endpoint.Query()
	queryString.Set("expiration", strconv.FormatInt(retryUnix, 10))
	endpoint.RawQuery = queryString.Encode()
	req, err := http.NewRequest("GET", endpoint.String(), nil)
	if err != nil {
		return errors.Wrap(err, "PushDevice")
	}
	req.SetBasicAuth("micromdm", utils.MicroMDMAPIKey())

	resp, err := client.Do(req)
	if err != nil {
		return errors.Wrap(err, "PushDevice")
	}

	err = resp.Body.Close()
	if err != nil {
		return errors.Wrap(err, "PushDevice")
	}

	InfoLogger(LogHolder{DeviceUDID: device.UDID, Message: "Sent push to device"})

	return nil
}

// pushDeviceWithClient sends a push notification via NanoMDM using the provided client
func pushDeviceWithClient(nanoClient *mdm.NanoMDMClient, udid string) error {
	InfoLogger(LogHolder{DeviceUDID: udid, Message: "Sending push to device via NanoMDM"})

	resp, err := nanoClient.Push(udid)
	if err != nil {
		return errors.Wrap(err, "PushDevice")
	}

	pushErr, _ := resp.ErrorsForID(udid)
	if pushErr != "" {
		return errors.Errorf("push failed: %s", pushErr)
	}

	InfoLogger(LogHolder{DeviceUDID: udid, Message: "Sent push to device via NanoMDM"})
	return nil
}
