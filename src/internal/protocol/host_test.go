package protocol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBackoffBaseDelay(t *testing.T) {
	min := 5 * time.Second
	max := 2 * time.Minute

	testCases := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{name: "zero attempt defaults to min", attempt: 0, want: min},
		{name: "first attempt returns min", attempt: 1, want: min},
		{name: "second attempt doubles", attempt: 2, want: 2 * min},
		{name: "third attempt doubles again", attempt: 3, want: 4 * min},
		{name: "doubling capped at max", attempt: 6, want: max},
	}

	for _, tc := range testCases {
		got := backoffBaseDelay(tc.attempt, min, max)
		if got != tc.want {
			t.Fatalf("%s: backoffBaseDelay(%d) = %s, want %s", tc.name, tc.attempt, got, tc.want)
		}
	}
}

func TestBackoffDelay_ReturnsWithinExpectedRange(t *testing.T) {
	min := 5 * time.Second
	max := 2 * time.Minute

	for i := 0; i < 100; i++ {
		got := backoffDelay(1, min, max)
		base := backoffBaseDelay(1, min, max)
		jitterMax := base / 3

		assert.GreaterOrEqual(t, got, base, "backoffDelay should be >= base")
		assert.Less(t, got, base+jitterMax, "backoffDelay should be < base + base/3")
	}
}

func TestBackoffDelay_NeverExceedsMax(t *testing.T) {
	min := 5 * time.Second
	max := 2 * time.Minute

	for attempt := 0; attempt < 20; attempt++ {
		for i := 0; i < 50; i++ {
			got := backoffDelay(attempt, min, max)
			// The jitter adds up to base/3 on top of base, and base is capped at max.
			// So the absolute maximum is max + max/3.
			absoluteMax := max + max/3
			assert.LessOrEqual(t, got, absoluteMax,
				"backoffDelay(attempt=%d) = %s should not exceed max + jitter (%s)", attempt, got, absoluteMax)
		}
	}
}

func TestIsTransientNetworkError_NilError(t *testing.T) {
	assert.False(t, isTransientNetworkError(nil))
}

func TestIsTransientNetworkError_DeadlineExceeded(t *testing.T) {
	assert.True(t, isTransientNetworkError(context.DeadlineExceeded))
}

func TestIsTransientNetworkError_Canceled(t *testing.T) {
	assert.True(t, isTransientNetworkError(context.Canceled))
}

func TestIsTransientNetworkError_RegularError(t *testing.T) {
	err := errors.New("some error")
	assert.False(t, isTransientNetworkError(err))
}

func TestWaitFor_ReturnsTrue(t *testing.T) {
	ctx := context.Background()
	result := waitFor(ctx, 10*time.Millisecond)
	assert.True(t, result, "waitFor should return true when context is not cancelled")
}

func TestWaitFor_ReturnsFalseOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	result := waitFor(ctx, 10*time.Second)
	assert.False(t, result, "waitFor should return false when context is cancelled")
}

func TestWaitFor_ZeroDuration(t *testing.T) {
	ctx := context.Background()
	result := waitFor(ctx, 0)
	assert.True(t, result, "waitFor with zero duration should return true immediately")
}
