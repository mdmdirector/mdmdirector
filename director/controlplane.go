package director

import (
	"context"
	"time"

	"github.com/bsm/redislock"
	"github.com/pkg/errors"
)

const (
	// controlPlaneLockKey is the shared Redis key guarding the periodic control-plane
	// scan (device push scheduling + cleanup). Only the pod holding it runs the scan on
	// a given tick; every other pod skips. This is what makes the control plane
	// single-flight across replicas without any leader election.
	controlPlaneLockKey = "mdmdirector:control-plane"

	// controlPlaneLockTTL is how long the lock is held before it must be refreshed. A
	// scan can run for many minutes (pushAll paces itself with ~1m sleeps per chunk), so
	// a background refresher keeps the lock alive while the holder works. If the holder
	// dies mid-scan, the lock expires within this TTL and another pod takes over on its
	// next tick -- automatic failover, no SPOF.
	controlPlaneLockTTL = 30 * time.Second
)

// withControlPlaneLock runs fn only if this process can acquire the fleet-wide
// control-plane lock. If another pod already holds it, fn is skipped and (false, nil)
// is returned. While fn runs, a goroutine refreshes the lock periodically so a long
// scan does not lose it; the lock is released when fn returns. Returns whether fn ran
// and any error from acquiring the lock or from fn itself.
func withControlPlaneLock(ctx context.Context, rc redislock.RedisClient, fn func(context.Context) error) (bool, error) {
	locker := redislock.New(rc)
	lock, err := locker.Obtain(ctx, controlPlaneLockKey, controlPlaneLockTTL, nil)
	if errors.Is(err, redislock.ErrNotObtained) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrap(err, "withControlPlaneLock: obtain")
	}
	defer func() {
		if rerr := lock.Release(context.Background()); rerr != nil && !errors.Is(rerr, redislock.ErrLockNotHeld) {
			ErrorLogger(LogHolder{Message: "control-plane lock release: " + rerr.Error()})
		}
	}()

	// Keep the lock alive for the duration of fn.
	refreshCtx, stopRefresh := context.WithCancel(ctx)
	defer stopRefresh()
	go func() {
		ticker := time.NewTicker(controlPlaneLockTTL / 3)
		defer ticker.Stop()
		for {
			select {
			case <-refreshCtx.Done():
				return
			case <-ticker.C:
				if rerr := lock.Refresh(refreshCtx, controlPlaneLockTTL, nil); rerr != nil {
					DebugLogger(LogHolder{Message: "control-plane lock refresh: " + rerr.Error()})
				}
			}
		}
	}()

	return true, fn(ctx)
}
