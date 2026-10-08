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
// an Error status.
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
// signs the payload and dedupes against anything already pending. attempt_count on the
// new command is the failed command's count plus one, so the chain is bounded by
// -install-profile-retries.
//
// Each attempt waits attempt_count+1 seconds before re-sending (1s, 2s, 3s, ...), inside
// the webhook request. That holds the device's check-in for that long, which is the
// point: it gives a short-lived fault on the device room to clear before the next try
// lands. With the default of 5 the longest single wait is 5s, the whole chain ~15s.
// Anything that outlasts the chain is picked up by VerifyMDMProfiles on the next
// scheduled ProfileList, see erroredInstallProfiles.
//
// The retry is skipped when:
//
//   - retries are disabled (-install-profile-retries 0)
//   - the failed command has already been retried that many times
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
		logFields.Message = fmt.Sprintf("InstallProfile failed on the device after %d retries, not retrying again", failed.AttemptCount)
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
		pushed, err = PushProfiles([]types.Device{device}, []types.DeviceProfile{deviceProfile}, false)
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
		pushed, err = PushSharedProfiles([]types.Device{device}, []types.SharedProfile{sharedProfile}, false)
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

	attempt := failed.AttemptCount + 1
	var errs []error
	for i := range pushed {
		err := db.DB.Model(&types.Command{}).Where("command_uuid = ?", pushed[i].CommandUUID).Update("attempt_count", attempt).Error
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "record attempt_count on %s", pushed[i].CommandUUID))
		}
	}

	logFields.Message = fmt.Sprintf("InstallProfile failed on the device, re-sent (retry %d of %d)", attempt, limit)
	InfoLogger(logFields)
	return intErrors.Join(errs...)
}

// retrySleep is the wait before a retry is re-sent. A variable so tests can stub it.
//
//nolint:gochecknoglobals
var retrySleep = time.Sleep

// erroredInstallProfiles returns, for each profile identifier on the device, the content
// hash of its most recent InstallProfile when that command's latest result was Error.
// Identifiers whose latest InstallProfile is pending, acknowledged, or anything other
// than Error are left out.
//
// VerifyMDMProfiles uses this to reinstall a profile the device lists as present but
// whose last install failed. These are fresh pushes with attempt_count 0: the
// scheduled verification runs rarely enough that each one is allowed its own short
// webhook retry chain.
func erroredInstallProfiles(udid string) (map[string]string, error) {
	type lastInstall struct {
		Identifier  string
		ContentHash string
		Status      string
	}
	var rows []lastInstall
	err := db.DB.Raw(
		`SELECT DISTINCT ON (identifier) identifier, content_hash, status FROM commands `+
			`WHERE device_ud_id = ? AND request_type = ? ORDER BY identifier, updated_at DESC`,
		udid, "InstallProfile",
	).Scan(&rows).Error
	if err != nil {
		return nil, errors.Wrap(err, "erroredInstallProfiles")
	}
	errored := make(map[string]string)
	for _, r := range rows {
		if r.Status == "Error" {
			errored[r.Identifier] = r.ContentHash
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
// profile, at its current content, came back with Error. An Error for older content
// doesn't count: that content is no longer what would be re-sent.
func lastInstallErrored(errored map[string]string, profile ProfileForVerification) bool {
	hash, ok := errored[profile.PayloadIdentifier]
	return ok && strings.EqualFold(hash, profile.HashedPayloadUUID)
}
