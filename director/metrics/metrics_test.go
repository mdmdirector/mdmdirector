package metrics

import (
	"errors"
	"testing"

	prometheustestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestCheckinRequests_incrementsByLabels(t *testing.T) {
	counter := CheckinRequests("TokenUpdate", "success")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))

	// Different label tuple is a distinct series.
	other := CheckinRequests("Authenticate", "error")
	assert.Equal(t, float64(0), prometheustestutil.ToFloat64(other))
}

func TestCommandResults_incrementsByLabels(t *testing.T) {
	counter := CommandResults("Acknowledged", "success")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))

	// Different label tuple is a distinct series.
	other := CommandResults("Error", "error")
	assert.Equal(t, float64(0), prometheustestutil.ToFloat64(other))
}

func TestPushRequests_incrementsByResult(t *testing.T) {
	counter := PushRequests("success")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))
}

func TestProfileOperations_incrementsByLabels(t *testing.T) {
	counter := ProfileOperations("device", "pushed", "success")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))

	// Different label tuple is a distinct series.
	other := ProfileOperations("shared", "deleted", "error")
	assert.Equal(t, float64(0), prometheustestutil.ToFloat64(other))
}

func TestApplicationOperations_incrementsByLabels(t *testing.T) {
	counter := ApplicationOperations("device", "pushed", "success")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))

	// Different label tuple is a distinct series.
	other := ApplicationOperations("shared", "pushed", "error")
	assert.Equal(t, float64(0), prometheustestutil.ToFloat64(other))
}

func TestEnqueueRequests_incrementsByLabels(t *testing.T) {
	counter := EnqueueRequests("InstallProfile", "success")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))

	other := EnqueueRequests("DeviceLock", "error")
	assert.Equal(t, float64(0), prometheustestutil.ToFloat64(other))
}

func TestProfileVerificationMismatches_incrementsByScope(t *testing.T) {
	counter := ProfileVerificationMismatches("device")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))

	other := ProfileVerificationMismatches("shared")
	assert.Equal(t, float64(0), prometheustestutil.ToFloat64(other))
}

func TestInitialTasks_incrementsByResult(t *testing.T) {
	counter := InitialTasks("success")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))

	other := InitialTasks("lease_contention")
	assert.Equal(t, float64(0), prometheustestutil.ToFloat64(other))
}

func TestDDMDeclarationWrites_incrementsByLabels(t *testing.T) {
	counter := DDMDeclarationWrites("configuration", "profile", "put", "success")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))

	other := DDMDeclarationWrites("configuration", "application", "touch", "error")
	assert.Equal(t, float64(0), prometheustestutil.ToFloat64(other))
}

func TestDDMSetMembershipChanges_incrementsByLabels(t *testing.T) {
	counter := DDMSetMembershipChanges("activation", "", "put", "success")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))

	other := DDMSetMembershipChanges("configuration", "profile", "delete", "error")
	assert.Equal(t, float64(0), prometheustestutil.ToFloat64(other))
}

func TestDDMNotify_incrementsByResult(t *testing.T) {
	counter := DDMNotify("success")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))

	other := DDMNotify("error")
	assert.Equal(t, float64(0), prometheustestutil.ToFloat64(other))
}

func TestPinEscrow_incrementsByResult(t *testing.T) {
	counter := PinEscrow("success")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))
}

func TestProfileDownloadRequests_incrementsByLabels(t *testing.T) {
	counter := ProfileDownloadRequests("device", "success")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))

	// Different label tuple is a distinct series.
	other := ProfileDownloadRequests("none", "not_found")
	assert.Equal(t, float64(0), prometheustestutil.ToFloat64(other))
}

func TestProfileAPIRequests_incrementsByLabels(t *testing.T) {
	counter := ProfileAPIRequests("post", "success")
	before := prometheustestutil.ToFloat64(counter)

	counter.Inc()

	assert.Equal(t, before+1, prometheustestutil.ToFloat64(counter))

	other := ProfileAPIRequests("delete", "error")
	assert.Equal(t, float64(0), prometheustestutil.ToFloat64(other))
}

func TestDDMEnabledDevices_setsValue(t *testing.T) {
	gauge := DDMEnabledDevices()

	gauge.Set(42)

	assert.Equal(t, float64(42), prometheustestutil.ToFloat64(gauge))
}

func TestResultLabel(t *testing.T) {
	assert.Equal(t, "success", ResultLabel(200))
	assert.Equal(t, "success", ResultLabel(399))
	assert.Equal(t, "error", ResultLabel(400))
	assert.Equal(t, "error", ResultLabel(500))
}

func TestResultFromError(t *testing.T) {
	assert.Equal(t, "success", ResultFromError(nil))
	assert.Equal(t, "error", ResultFromError(errors.New("boom")))
}
