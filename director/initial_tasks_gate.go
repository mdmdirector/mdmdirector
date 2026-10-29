package director

import (
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
)

// Paths that defer while a device's initial tasks are pending, as the path label of
// mdmdirector_pushes_deferred_total.
const (
	deferPathIdleInfoRequest = "idle_info_request"
	deferPathPushOnNewBuild  = "push_on_new_build"
	deferPathDDMActivate     = "ddm_activate"
	deferPathDDMEnable       = "ddm_enable"
	deferPathProfilePost     = "profile_post"
)

// initialTasksPending reports whether a push or info request to the device has to wait
// for RunInitialTasks, and records that it was deferred when so.
//
// RunInitialTasks owns the first push of an enrollment. Before it pushes it clears the
// previous enrollment's declarations from the device's KMFDDM set (resetDDMForEnrollment),
// and nanomdm's async webhooks mean the device is already polling while that runs. Any
// other path that pushes via DDM in that window re-associates the set and notifies, and
// the device syncs declarations the cleanup has not reached yet. Non-DDM pushes are
// harmless but redundant: RunInitialTasks pushes everything anyway.
func initialTasksPending(device types.Device, path string) bool {
	if device.InitialTasksRun {
		return false
	}
	InfoLogger(LogHolder{DeviceUDID: device.UDID, DeviceSerial: device.SerialNumber, Message: "Initial tasks pending; deferring " + path})
	if utils.Prometheus() {
		metrics.PushesDeferred(path).Inc()
	}
	return true
}
