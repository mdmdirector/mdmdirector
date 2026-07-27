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
		// pushQueue is nil; the cancelled context guarantees we return before
		// ever dereferencing it.
		ScheduledCheckin(ctx, nil, time.Minute)
		close(done)
	}()

	select {
	case <-done:
		// returned promptly, as expected
	case <-time.After(2 * time.Second):
		t.Fatal("ScheduledCheckin did not return after context cancellation")
	}
}

// TestScheduledCheckinReturnsWhileWaitingForDevices verifies that a shutdown while
// still waiting for the initial device fetch also unblocks the wait loop.
func TestScheduledCheckinReturnsWhileWaitingForDevices(t *testing.T) {
	prev := DevicesFetchedFromMDM
	DevicesFetchedFromMDM = false
	defer func() { DevicesFetchedFromMDM = prev }()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		ScheduledCheckin(ctx, nil, time.Minute)
		close(done)
	}()

	// Give the goroutine a moment to enter the wait loop, then signal shutdown.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ScheduledCheckin did not return from the device-fetch wait loop on cancellation")
	}
}
