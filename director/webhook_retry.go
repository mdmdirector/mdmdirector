package director

import (
	"fmt"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
)

// staleConnectionAttempts bounds how many times a webhook step runs when it keeps
// failing on a dead pooled connection. A sidecar reset kills every idle connection at
// once, and database/sql discards only the one that failed, so the second attempt can
// draw another dead one. Each failure on a dead connection is immediate, so there is no
// backoff between attempts.
const staleConnectionAttempts = 3

// Webhook step names, used as the step label of mdmdirector_webhook_step_retries_total.
const (
	stepResetDevice    = "reset_device"
	stepSetTokenUpdate = "set_token_update"
	stepUpdateDevice   = "update_device"
	stepUpdateCommand  = "update_command"
)

// retryOnStaleConnection runs a webhook handler step and runs it again while it fails
// because the pooled Postgres connection was dead.
//
// nanomdm sends webhooks from a goroutine after it has stored the check-in or command
// result and answered the device, and it only logs a failed webhook. Nothing redelivers
// the event, so a step that fails here is lost. Retrying is the only recovery.
//
// Use it only for steps that are safe to run twice: each must write absolute values (no
// increments) and must not send commands or pushes before its last write. A write that
// fails on a reset connection may already have been applied, so the next attempt can
// apply it again.
func retryOnStaleConnection(step string, lh LogHolder, fn func() error) error {
	err := fn()
	attempt := 1
	for ; attempt < staleConnectionAttempts && db.IsStaleConnection(err); attempt++ {
		lh.Message = fmt.Sprintf("webhook step %s failed on a stale database connection (attempt %d of %d), retrying: %v", step, attempt, staleConnectionAttempts, err)
		WarnLogger(lh)
		err = fn()
	}
	if attempt == 1 {
		return err
	}

	result := "recovered"
	if err != nil {
		result = "failed"
	}
	if utils.Prometheus() {
		metrics.WebhookStepRetries(step, result).Inc()
	}
	return err
}

// updateDeviceRetrying is UpdateDevice run under retryOnStaleConnection. UpdateDevice is
// a lookup followed by a create or an assign of absolute values, so a second run after a
// create that landed finds the row and updates it instead.
func updateDeviceRetrying(device types.Device) (*types.Device, error) {
	var current *types.Device
	lh := LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber}
	err := retryOnStaleConnection(stepUpdateDevice, lh, func() error {
		var err error
		current, err = UpdateDevice(device)
		return err
	})
	return current, err
}
