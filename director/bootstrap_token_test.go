package director

import (
	"errors"
	"flag"
	"testing"

	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/types"
	prometheustestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeKeeper struct {
	keepErr   error
	kept      []string
	forgotten []string
}

func (f *fakeKeeper) KeepBootstrapToken(udid string) error {
	f.kept = append(f.kept, udid)
	return f.keepErr
}

func (f *fakeKeeper) ForgetBootstrapTokenKeep(udid string) error {
	f.forgotten = append(f.forgotten, udid)
	return nil
}

func withServerType(t *testing.T, serverType string) {
	t.Helper()
	if flag.Lookup("mdm-server-type") == nil {
		flag.String("mdm-server-type", "micromdm", "server type")
	}
	prev := flag.Lookup("mdm-server-type").Value.String()
	require.NoError(t, flag.Set("mdm-server-type", serverType))
	t.Cleanup(func() { _ = flag.Set("mdm-server-type", prev) })
}

func withKeeper(t *testing.T, k bootstrapTokenKeeper, err error) {
	t.Helper()
	prev := bootstrapTokenKeeperClient
	bootstrapTokenKeeperClient = func() (bootstrapTokenKeeper, error) { return k, err }
	t.Cleanup(func() { bootstrapTokenKeeperClient = prev })
}

func TestKeepBootstrapToken(t *testing.T) {
	device := types.Device{UDID: "UDID-1", SerialNumber: "SERIAL1"}

	t.Run("micromdm: no call", func(t *testing.T) {
		withServerType(t, "micromdm")
		k := &fakeKeeper{}
		withKeeper(t, k, nil)
		assert.False(t, keepBootstrapToken(device))
		assert.Empty(t, k.kept)
	})

	t.Run("nanomdm: marks", func(t *testing.T) {
		withServerType(t, string(mdm.ServerTypeNanoMDM))
		k := &fakeKeeper{}
		withKeeper(t, k, nil)
		before := prometheustestutil.ToFloat64(metrics.BootstrapTokenKeep("marked"))
		assert.True(t, keepBootstrapToken(device))
		assert.Equal(t, []string{"UDID-1"}, k.kept)
		assert.Equal(t, before+1, prometheustestutil.ToFloat64(metrics.BootstrapTokenKeep("marked")))
	})

	t.Run("nanomdm: unknown device is counted, not marked", func(t *testing.T) {
		withServerType(t, string(mdm.ServerTypeNanoMDM))
		withKeeper(t, &fakeKeeper{keepErr: mdm.ErrBootstrapTokenKeepUnknownDevice}, nil)
		before := prometheustestutil.ToFloat64(metrics.BootstrapTokenKeep("unknown_device"))
		assert.False(t, keepBootstrapToken(device))
		assert.Equal(t, before+1, prometheustestutil.ToFloat64(metrics.BootstrapTokenKeep("unknown_device")))
	})

	t.Run("nanomdm: endpoint error does not block", func(t *testing.T) {
		withServerType(t, string(mdm.ServerTypeNanoMDM))
		withKeeper(t, &fakeKeeper{keepErr: errors.New("status 404")}, nil)
		before := prometheustestutil.ToFloat64(metrics.BootstrapTokenKeep("error"))
		assert.False(t, keepBootstrapToken(device))
		assert.Equal(t, before+1, prometheustestutil.ToFloat64(metrics.BootstrapTokenKeep("error")))
	})

	t.Run("nanomdm: no client", func(t *testing.T) {
		withServerType(t, string(mdm.ServerTypeNanoMDM))
		withKeeper(t, nil, mdm.ErrClientNotInitialized)
		assert.False(t, keepBootstrapToken(device))
	})
}

func TestForgetBootstrapTokenKeep(t *testing.T) {
	k := &fakeKeeper{}
	withKeeper(t, k, nil)
	forgetBootstrapTokenKeep(types.Device{UDID: "UDID-2"})
	assert.Equal(t, []string{"UDID-2"}, k.forgotten)
}
