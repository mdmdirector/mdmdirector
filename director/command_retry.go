package director

import (
	intErrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

// retryErroredInstallProfile re-sends an InstallProfile the device just answered with
// an Error status. It is the fast tier of a two-tier retry; see erroredInstallProfiles
// for the slow tier that runs from VerifyMDMProfiles.
//
// Until now an Error was recorded on the Command row and nothing acted on it. The row
// doesn't block a later push (CommandInQueue only treats status "" / NotNow as
// pending), but nothing *initiates* one either: the next scheduled ProfileList only
// re-pushes a profile that is missing or whose UUID differs, and a profile POST only
// re-pushes when the content changed. A transient failure on the device can leave the
// profile half-installed in a way neither check can see, so the profile stays broken
// until something unrelated pushes it again.
//
// The command row doesn't carry the payload, so the retry rebuilds the InstallProfile
// from the profile that is still assigned to the device (device-specific first, then
// shared) and goes through the normal PushProfiles / PushSharedProfiles path, which
// signs the payload and dedupes against anything already pending. The new row is
// inserted with attempt_count = failed.AttemptCount + 1 (carried in the CommandPayload),
// so the count is right before the device can possibly answer. The fast tier stops once
// that count reaches -install-profile-retries.
//
// Each attempt waits attempt_count+1 seconds before re-sending (1s, 2s, 3s with the
// default of 3). The wait spaces the attempts out. It runs inside the webhook handler,
// so for that long it occupies one mdmdirector goroutine and holds nanomdm's webhook
// request open; the device is not waiting on it (nanomdm answers the device first and
// runs the webhook in a detached goroutine), and no lock, transaction or lease is held.
//
// The retry is skipped when:
//
//   - retries are disabled (-install-profile-retries 0)
//   - the failed command's attempt_count has reached the fast-tier limit (this is also
//     how a slow-tier push from VerifyMDMProfiles, numbered above the fast limit, gets
//     no fast chain behind it)
//   - the device is on DDM for profiles (its profiles are delivered as declarations,
//     not InstallProfile commands)
//   - the assigned profile's content has changed since the failed command was built
//     (the fresh content gets its own push through the normal paths)
//   - the profile is no longer assigned to the device
func retryErroredInstallProfile(device types.Device, commandUUID string) error {
	limit := utils.InstallProfileRetries()
	if limit <= 0 {
		return nil
	}

	var failed types.Command
	err := db.DB.Where("device_ud_id = ? AND command_uuid = ?", device.UDID, commandUUID).First(&failed).Error
	if err != nil {
		if intErrors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return errors.Wrap(err, "load failed command")
	}

	logFields := LogHolder{
		DeviceUDID:         device.UDID,
		DeviceSerial:       device.SerialNumber,
		CommandUUID:        commandUUID,
		CommandRequestType: failed.RequestType,
		ProfileIdentifier:  failed.Identifier,
	}

	if failed.AttemptCount >= limit {
		logFields.Message = fmt.Sprintf("InstallProfile failed on the device at attempt %d, fast retries exhausted (limit %d); the next ProfileList decides", failed.AttemptCount, limit)
		InfoLogger(logFields)
		return nil
	}

	if ddmForDevice(device) {
		logFields.Message = "InstallProfile failed on the device, not retrying: device profiles are managed via DDM"
		InfoLogger(logFields)
		return nil
	}

	// Backoff: 1s before the first retry, one more second for each after that.
	retrySleep(time.Duration(failed.AttemptCount+1) * time.Second)

	attempt := failed.AttemptCount + 1
	var pushed []types.Command

	var deviceProfile types.DeviceProfile
	err = db.DB.Where("device_ud_id = ? AND payload_identifier = ? AND installed = ?", device.UDID, failed.Identifier, true).First(&deviceProfile).Error
	switch {
	case err == nil:
		if deviceProfile.HashedPayloadUUID != failed.ContentHash {
			logFields.Message = "InstallProfile failed on the device, not retrying: profile content has changed since it was queued"
			InfoLogger(logFields)
			return nil
		}
		pushed, err = PushProfiles([]types.Device{device}, []types.DeviceProfile{deviceProfile}, false, withAttemptCount(attempt))
		if err != nil {
			return errors.Wrap(err, "re-push device profile")
		}
	case intErrors.Is(err, gorm.ErrRecordNotFound):
		var sharedProfile types.SharedProfile
		err = db.DB.Where("payload_identifier = ? AND installed = ?", failed.Identifier, true).First(&sharedProfile).Error
		if err != nil {
			if intErrors.Is(err, gorm.ErrRecordNotFound) {
				logFields.Message = "InstallProfile failed on the device, not retrying: profile is no longer assigned"
				InfoLogger(logFields)
				return nil
			}
			return errors.Wrap(err, "load shared profile")
		}
		if sharedProfile.HashedPayloadUUID != failed.ContentHash {
			logFields.Message = "InstallProfile failed on the device, not retrying: profile content has changed since it was queued"
			InfoLogger(logFields)
			return nil
		}
		pushed, err = PushSharedProfiles([]types.Device{device}, []types.SharedProfile{sharedProfile}, false, withAttemptCount(attempt))
		if err != nil {
			return errors.Wrap(err, "re-push shared profile")
		}
	default:
		return errors.Wrap(err, "load device profile")
	}

	if len(pushed) == 0 {
		// Deduped against a command that is already pending for this content.
		return nil
	}

	logFields.Message = fmt.Sprintf("InstallProfile failed on the device, re-sent (fast retry %d of %d)", attempt, limit)
	InfoLogger(logFields)
	return nil
}

// pushOption adjusts the CommandPayload PushProfiles / PushSharedProfiles build for each
// profile, just before it is sent.
type pushOption func(*types.CommandPayload)

// withAttemptCount records the retry position on the command row at insert time.
func withAttemptCount(n int) pushOption {
	return func(p *types.CommandPayload) { p.AttemptCount = n }
}

// retrySleep is the wait before a retry is re-sent. A variable so tests can stub it.
//
//nolint:gochecknoglobals
var retrySleep = time.Sleep

// erroredInstall is the most recent InstallProfile result for one profile identifier on
// a device, when that result was Error.
type erroredInstall struct {
	ContentHash  string
	AttemptCount int
}

// erroredInstallProfiles returns, for each profile identifier on the device whose most
// recent InstallProfile came back Error, that command's content hash and attempt_count.
// Identifiers whose latest InstallProfile is pending, acknowledged, or anything other
// than Error are left out.
//
// This drives the slow tier of the retry. The fast tier (retryErroredInstallProfile)
// re-sends from the webhook up to -install-profile-retries times, seconds apart. When
// that is exhausted the row is left at that count, and VerifyMDMProfiles, on each
// scheduled ProfileList, sends one more attempt numbered failed+1 while the count is
// below -install-profile-total-retries. Those are hours apart and, being numbered above
// the fast limit, get no fast chain of their own. Once the count reaches the total the
// profile is left alone until its content changes. With the defaults (3 fast, 5 total)
// that is the original send, 3 fast retries over ~6s, then 2 more one ProfileList
// interval apart each.
func erroredInstallProfiles(udid string) (map[string]erroredInstall, error) {
	type lastInstall struct {
		Identifier   string
		ContentHash  string
		Status       string
		AttemptCount int
	}
	var rows []lastInstall
	err := db.DB.Raw(
		`SELECT DISTINCT ON (identifier) identifier, content_hash, status, attempt_count FROM commands `+
			`WHERE device_ud_id = ? AND request_type = ? ORDER BY identifier, updated_at DESC`,
		udid, "InstallProfile",
	).Scan(&rows).Error
	if err != nil {
		return nil, errors.Wrap(err, "erroredInstallProfiles")
	}
	errored := make(map[string]erroredInstall)
	for _, r := range rows {
		if r.Status == "Error" {
			errored[r.Identifier] = erroredInstall{ContentHash: r.ContentHash, AttemptCount: r.AttemptCount}
		}
	}
	return errored, nil
}

// commandStatusRetriedViaDDM replaces Error on an InstallProfile row once VerifyMDMProfiles
// has re-asserted that profile through DDM. A device on DDM never gets another
// InstallProfile row for the identifier, so without this the Error row would stay the
// latest result forever and every scheduled ProfileList would touch the declaration again.
// The row is kept, with its original payload, for history; it just no longer counts as an
// outstanding failure.
const commandStatusRetriedViaDDM = "RetriedViaDDM"

// markErroredInstallsRetriedViaDDM records that the profiles in identifiers were re-sent to
// the device as declarations, so their failed InstallProfile rows stop being retried.
func markErroredInstallsRetriedViaDDM(udid string, identifiers []string) error {
	if len(identifiers) == 0 {
		return nil
	}
	err := db.DB.Model(&types.Command{}).
		Where("device_ud_id = ? AND request_type = ? AND status = ? AND identifier IN ?", udid, "InstallProfile", "Error", identifiers).
		Update("status", commandStatusRetriedViaDDM).Error
	if err != nil {
		return errors.Wrap(err, "markErroredInstallsRetriedViaDDM")
	}
	return nil
}

// lastInstallErrored reports whether the device's most recent InstallProfile for this
// profile, at its current content, came back with Error and is still below the total
// retry limit. When it is, the returned attempt is the count the next command should
// carry. An Error for older content doesn't count: that content is no longer what would
// be re-sent.
func lastInstallErrored(errored map[string]erroredInstall, profile ProfileForVerification, totalRetries int) (attempt int, retry bool) {
	last, ok := errored[profile.PayloadIdentifier]
	if !ok || !strings.EqualFold(last.ContentHash, profile.HashedPayloadUUID) {
		return 0, false
	}
	if last.AttemptCount >= totalRetries {
		return 0, false
	}
	return last.AttemptCount + 1, true
}
