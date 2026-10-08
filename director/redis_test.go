package director

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRedisFlags registers/sets the redis flags so redisOptions() works in tests.
func setupRedisFlags(t *testing.T, values map[string]string) {
	t.Helper()
	defaults := map[string]string{
		"redis-host":                 "localhost",
		"redis-port":                 "6379",
		"redis-password":             "",
		"redis-idle-timeout":         "240",
		"redis-max-conn-age":         "1800",
		"redis-idle-check-frequency": "60",
	}
	for name, def := range defaults {
		if flag.Lookup(name) == nil {
			switch name {
			case "redis-host", "redis-port", "redis-password":
				flag.String(name, def, name)
			default:
				flag.Int(name, 0, name)
			}
		}
		v, ok := values[name]
		if !ok {
			v = def
		}
		require.NoError(t, flag.Set(name, v))
	}
	if flag.Lookup("redis-tls") == nil {
		flag.Bool("redis-tls", false, "redis-tls")
	}
	require.NoError(t, flag.Set("redis-tls", "false"))
}

func TestRedisOptionsDefaults(t *testing.T) {
	setupRedisFlags(t, nil)

	opts := redisOptions()
	assert.Equal(t, "localhost:6379", opts.Addr)
	assert.Equal(t, 240*time.Second, opts.IdleTimeout)
	assert.Equal(t, 1800*time.Second, opts.MaxConnAge)
	assert.Equal(t, 60*time.Second, opts.IdleCheckFrequency)
	assert.Nil(t, opts.TLSConfig)
}

func TestRedisOptionsOverridesAndDisable(t *testing.T) {
	setupRedisFlags(t, map[string]string{
		"redis-idle-timeout":         "-1",
		"redis-max-conn-age":         "0",
		"redis-idle-check-frequency": "-1",
	})

	opts := redisOptions()
	assert.Equal(t, time.Duration(-1), opts.IdleTimeout, "-1 must stay the go-redis disabled sentinel")
	assert.Equal(t, time.Duration(0), opts.MaxConnAge)
	assert.Equal(t, time.Duration(-1), opts.IdleCheckFrequency)
}
