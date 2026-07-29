package director

import (
	"time"

	"github.com/go-redis/redis_rate/v9"
	"github.com/vmihailenco/taskq/v3"
)

// PushQueueName is the taskq queue name for scheduled device pushes.
const PushQueueName = "pushnotifications"

// PushQueueOptions builds the options for the push queue.
//
// perMinute is a fleet-wide ceiling on how fast the consumers may reserve push messages,
// enforced through Redis, so it holds across every replica rather than per pod. A
// non-positive value leaves the queue unlimited.
//
// This is a ceiling, not a pacing mechanism -- pushAll's delivery spread (PUSH_SPREAD) is
// what normally keeps the rate low. The limit covers what the spread cannot:
//
//   - retries. A failed push is released with backoff and re-reserved; those retries
//     carry none of the original jitter, so a NanoMDM or APNs outage otherwise means
//     every push in the window retrying with no throughput bound at all.
//   - misconfiguration. Nothing else stops a too-small PUSH_SPREAD, or an unusually
//     large batch of due devices, from saturating the MDM server.
//
// Without it the only bound is worker count, which defaults to 32*NumCPU per replica.
//
// When messages come due faster than the limit allows, taskq's limiter blocks on
// reservation (it sleeps for the limiter's RetryAfter) rather than dropping work, so the
// backlog drains late instead of being lost. It does not compound across scans either:
// pushAll reserves NextPush for the whole delivery window plus one cadence, so a device
// whose push is still pending is not due again on the next scan.
// ImpliedFleetCapacity is how many devices a per-minute ceiling can carry within a spread
// window. Past this, the rate limit rather than PUSH_SPREAD is what paces delivery, and
// pushes run past the end of the window. Zero means unlimited.
func ImpliedFleetCapacity(perMinute int, pushSpread time.Duration) int {
	if perMinute <= 0 {
		return 0
	}
	return int(float64(perMinute) * pushSpread.Minutes())
}

// impliedPushRate is the per-minute delivery rate needed to spread count pushes evenly
// across the window. Compare it against PUSH_RATE_LIMIT.
func impliedPushRate(count int, pushSpread time.Duration) float64 {
	if pushSpread <= 0 {
		return 0
	}
	return float64(count) / pushSpread.Minutes()
}

func PushQueueOptions(name string, redisClient taskq.Redis, perMinute int) *taskq.QueueOptions {
	opts := &taskq.QueueOptions{
		Name:  name,
		Redis: redisClient,
	}

	if perMinute > 0 {
		opts.RateLimit = redis_rate.PerMinute(perMinute)
	}

	return opts
}
