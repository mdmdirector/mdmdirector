package director

import (
	"context"
	"testing"
	"time"
)

// TestScheduledCheckinReturnsOnContextCancel verifies that ScheduledCheckin exits
// promptly when its context is cancelled, instead of looping forever. The device
// fetch flag is forced true so we skip the initial wait loop, and the context is
// cancelled before we start so no scheduling pass (which would touch the DB) runs.
func TestScheduledCheckinReturnsOnContextCancel(t *testing.T) {
	prev := DevicesFetchedFromMDM
	DevicesFetchedFromMDM = true
	defer func() { DevicesFetchedFromMDM = prev }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		// rc and pushQueue are nil; the cancelled context guarantees we return
		// before ever dereferencing them.
		ScheduledCheckin(ctx, nil, nil, time.Minute, time.Minute)
		close(done)
	}()

	select {
	case <-done:
		// returned promptly, as expected
	case <-time.After(2 * time.Second):
		t.Fatal("ScheduledCheckin did not return after context cancellation")
	}
}

// TestScheduledCheckinReturnsWhenCancelledBeforeDeviceFetch verifies that a shutdown
// signal is honoured even before any devices have been fetched: the run guard skips the
// scan (so the nil redis client is never dereferenced) and the loop returns promptly.
func TestScheduledCheckinReturnsWhenCancelledBeforeDeviceFetch(t *testing.T) {
	prev := DevicesFetchedFromMDM
	DevicesFetchedFromMDM = false
	defer func() { DevicesFetchedFromMDM = prev }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		ScheduledCheckin(ctx, nil, nil, time.Minute, time.Minute)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ScheduledCheckin did not return on cancellation before the first device fetch")
	}
}
