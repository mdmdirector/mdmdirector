package director

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushQueueOptionsSetsFleetWideRateLimit(t *testing.T) {
	opts := PushQueueOptions(PushQueueName, nil, 600)

	assert.Equal(t, PushQueueName, opts.Name)

	require.False(t, opts.RateLimit.IsZero(), "rate limit must be set when perMinute > 0")
	assert.Equal(t, 600, opts.RateLimit.Rate)
	assert.Equal(t, time.Minute, opts.RateLimit.Period)
}

func TestPushQueueOptionsUnlimitedWhenNotPositive(t *testing.T) {
	for _, perMinute := range []int{0, -1} {
		opts := PushQueueOptions(PushQueueName, nil, perMinute)
		assert.True(
			t, opts.RateLimit.IsZero(),
			"perMinute %d should leave the queue unlimited", perMinute,
		)
	}
}

// TestPushQueueOptionsRateLimitCoversRetries documents why the limit exists on the queue
// rather than in pushAll: taskq applies it on the consumer's reservation path, so it
// bounds retries and any other re-delivery, none of which carry pushAll's jitter.
func TestPushQueueOptionsRateLimitCoversRetries(t *testing.T) {
	opts := PushQueueOptions(PushQueueName, nil, 60)

	// A limiter is only constructed by taskq when both a limit and a Redis client are
	// present, which is what makes the ceiling fleet-wide rather than per replica.
	require.False(t, opts.RateLimit.IsZero())
	assert.Nil(t, opts.Redis, "no client passed in this test, so taskq builds no limiter")
	assert.Nil(t, opts.RateLimiter)
}

// TestSpreadAndCeilingAreComplementary pins the relationship between the two knobs:
// PUSH_SPREAD sets the operating point, PUSH_RATE_LIMIT is the ceiling above it. If the
// implied rate exceeds the ceiling, the ceiling becomes the pacer and the spread stops
// meaning anything -- which is what the startup and per-scan log lines expose.
func TestSpreadAndCeilingAreComplementary(t *testing.T) {
	spread := 90 * time.Minute
	const perMinute = 600

	capacity := ImpliedFleetCapacity(perMinute, spread)
	assert.Equal(t, 54000, capacity, "600/min over 90m carries 54k devices")

	// A 10k fleet spread over 90m sits far below the ceiling, leaving headroom for
	// retries and on-demand pushes.
	assert.InDelta(t, 111.1, impliedPushRate(10_000, spread), 0.1)
	assert.Less(t, impliedPushRate(capacity, spread), float64(perMinute+1))

	// Past capacity the ceiling is what paces delivery.
	assert.Greater(t, impliedPushRate(capacity*2, spread), float64(perMinute))

	// Unlimited and degenerate cases.
	assert.Zero(t, ImpliedFleetCapacity(0, spread))
	assert.Zero(t, impliedPushRate(100, 0))
}
