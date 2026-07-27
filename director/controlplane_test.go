package director

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/bsm/redislock"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

// TestWithControlPlaneLockSkipsWhenHeld verifies that when one replica already holds the
// control-plane lock, a second replica's attempt is skipped (fn does not run). This is
// the core single-flight guarantee that makes the control plane safe under N replicas.
func TestWithControlPlaneLockSkipsWhenHeld(t *testing.T) {
	rc := newTestRedis(t)
	ctx := context.Background()

	// Replica A grabs and holds the lock.
	held, err := redislock.New(rc).Obtain(ctx, controlPlaneLockKey, controlPlaneLockTTL, nil)
	require.NoError(t, err)
	defer func() { _ = held.Release(ctx) }()

	// Replica B attempts while A holds it: fn must not run.
	ran, err := withControlPlaneLock(ctx, rc, func(context.Context) error {
		t.Fatal("control-plane body ran while another replica held the lock")
		return nil
	})
	assert.NoError(t, err)
	assert.False(t, ran)
}

// TestWithControlPlaneLockFailover verifies that once the current holder releases the
// lock (e.g. it finished, or died and the TTL expired), another replica can acquire it
// and run on the next tick. No fixed owner, automatic failover, no leader election.
func TestWithControlPlaneLockFailover(t *testing.T) {
	rc := newTestRedis(t)
	ctx := context.Background()

	held, err := redislock.New(rc).Obtain(ctx, controlPlaneLockKey, controlPlaneLockTTL, nil)
	require.NoError(t, err)

	// While held, another attempt is skipped.
	ran, err := withControlPlaneLock(ctx, rc, func(context.Context) error { return nil })
	require.NoError(t, err)
	require.False(t, ran)

	// Holder goes away.
	require.NoError(t, held.Release(ctx))

	// Now a replica acquires the lock and runs the body.
	var bodyRan bool
	ok, err := withControlPlaneLock(ctx, rc, func(context.Context) error {
		bodyRan = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, bodyRan)
}

// TestWithControlPlaneLockReleasesAfterRun verifies the lock is released when fn returns,
// so the very next scan (this replica or another) can acquire it again.
func TestWithControlPlaneLockReleasesAfterRun(t *testing.T) {
	rc := newTestRedis(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		ran, err := withControlPlaneLock(ctx, rc, func(context.Context) error { return nil })
		require.NoError(t, err)
		assert.True(t, ran, "iteration %d should acquire the lock after the previous run released it", i)
	}
}
