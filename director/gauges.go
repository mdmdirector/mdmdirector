package director

import (
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/types"
)

// gaugePollInterval is how often each replica refreshes the fleet gauges. Every replica
// polls, and counting device_profiles is a full scan of a large table, so this is kept
// well above the scrape interval; the counts only move as devices enroll or profiles change.
const gaugePollInterval = 5 * time.Minute

// PollGauges starts background pollers that populate - mdmdirector_devices_total,
// mdmdirector_profiles_total and mdmdirector_ddm_enabled_devices_total
func PollGauges() {
	go pollDevices()
	go pollProfiles()
	go pollDDMOptIns()
}

func pollDevices() {
	for range time.Tick(gaugePollInterval) {
		var count int64
		if err := db.DB.Model(&types.Device{}).Count(&count).Error; err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			continue
		}
		metrics.DevicesTotal().Set(float64(count))
	}
}

func pollProfiles() {
	for range time.Tick(gaugePollInterval) {
		setProfilesGauge("shared", &types.SharedProfile{})
		setProfilesGauge("device", &types.DeviceProfile{})
	}
}

// pollDDMOptIns tracks how far the per-device DDM rollout has progressed
func pollDDMOptIns() {
	for range time.Tick(gaugePollInterval) {
		var count int64
		if err := db.DB.Model(&DDMOptIn{}).Count(&count).Error; err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			continue
		}
		metrics.DDMEnabledDevices().Set(float64(count))
	}
}

func setProfilesGauge(scope string, model interface{}) {
	// Both counts come from one pass over the table rather than one scan each. Rows with
	// a NULL installed are left out, as the per-value counts did.
	var rows []struct {
		Installed bool
		Count     int64
	}
	if err := db.DB.Model(model).Select("installed, count(*) AS count").Where("installed IS NOT NULL").Group("installed").Scan(&rows).Error; err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		return
	}
	var installed, uninstalled int64
	for _, row := range rows {
		if row.Installed {
			installed = row.Count
		} else {
			uninstalled = row.Count
		}
	}
	metrics.ProfilesTotal(scope, "true").Set(float64(installed))
	metrics.ProfilesTotal(scope, "false").Set(float64(uninstalled))
}
