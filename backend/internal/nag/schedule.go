// Package nag defines the retry schedule for unacknowledged pages — the
// product's heartbeat. Attempt 0 fires immediately on page creation; each
// completed attempt enqueues the next with the delay below until the page is
// acknowledged or expires.
package nag

import "time"

// Delays[i] is the wait between attempt i and attempt i+1. All values must
// stay ≤ 900s (SQS DelaySeconds maximum).
var Delays = []time.Duration{
	30 * time.Second,
	30 * time.Second,
	60 * time.Second,
	2 * time.Minute,
	5 * time.Minute,
	5 * time.Minute,
	5 * time.Minute,
	5 * time.Minute,
	5 * time.Minute,
	5 * time.Minute, // overshoot past Expiry is fine: the pager expires at fire time
}

// Expiry is how long a page nags before giving up as expired.
const Expiry = 30 * time.Minute

// NextDelay returns the wait before the given upcoming attempt, and false when
// the schedule is exhausted (attempt numbers start at 0; attempt 0 has no
// preceding delay and is enqueued with delay 0).
func NextDelay(nextAttempt int) (time.Duration, bool) {
	if nextAttempt <= 0 {
		return 0, true
	}
	i := nextAttempt - 1
	if i >= len(Delays) {
		return 0, false
	}
	return Delays[i], true
}
