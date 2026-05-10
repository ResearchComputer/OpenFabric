package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// timingEnabled is read once at startup (InitTiming). When false, stageTimer
// methods are no-ops and the request hot path makes no time.Now() calls.
var timingEnabled bool

// InitTiming caches observability.timing_headers. Call once during server startup.
func InitTiming() {
	timingEnabled = viper.GetBool("observability.timing_headers")
}

// stage is a single (name, duration_ms) pair.
type stage struct {
	name string
	ms   float64
}

// stageTimer accumulates stages for emission as a Server-Timing header.
// All methods are no-ops when timingEnabled is false.
type stageTimer struct {
	enabled bool
	last    time.Time
	stages  []stage
}

func newStageTimer() *stageTimer {
	if !timingEnabled {
		return &stageTimer{}
	}
	return &stageTimer{enabled: true, last: time.Now()}
}

// Mark records the duration since the previous Mark (or construction).
func (s *stageTimer) Mark(name string) {
	if !s.enabled {
		return
	}
	now := time.Now()
	ms := float64(now.Sub(s.last).Microseconds()) / 1000.0
	s.stages = append(s.stages, stage{name: name, ms: ms})
	s.last = now
}

// AddRaw appends a stage with a pre-computed duration. Used to fold in
// stages reported by an upstream worker.
func (s *stageTimer) AddRaw(name string, ms float64) {
	if !s.enabled {
		return
	}
	s.stages = append(s.stages, stage{name: name, ms: ms})
}

// Header returns the Server-Timing-formatted string. Empty if no stages or disabled.
func (s *stageTimer) Header() string {
	if !s.enabled || len(s.stages) == 0 {
		return ""
	}
	parts := make([]string, len(s.stages))
	for i, st := range s.stages {
		parts[i] = fmt.Sprintf("%s;dur=%.3f", st.name, st.ms)
	}
	return strings.Join(parts, ", ")
}

// mergeWorkerTiming reads X-Otela-Worker-Timing from an upstream response
// and appends each stage to the timer with a "worker_" prefix. No-op when
// the timer is disabled or the header is absent.
func mergeWorkerTiming(st *stageTimer, resp *http.Response) {
	if !st.enabled {
		return
	}
	h := resp.Header.Get("X-Otela-Worker-Timing")
	if h == "" {
		return
	}
	for _, ws := range parseServerTiming(h) {
		st.AddRaw("worker_"+ws.name, ws.ms)
	}
}

// parseServerTiming parses a Server-Timing-style header value into stages.
// Used by the head to merge worker-reported stages into its own header.
// Tolerates malformed input: missing dur is recorded as 0.0; unparseable
// numbers become 0.0; empty input returns nil.
func parseServerTiming(header string) []stage {
	if header == "" {
		return nil
	}
	var result []stage
	for _, entry := range strings.Split(header, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ";", 2)
		name := strings.TrimSpace(parts[0])
		var ms float64
		if len(parts) == 2 {
			for _, p := range strings.Split(parts[1], ";") {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(p, "dur=") {
					if v, err := strconv.ParseFloat(p[4:], 64); err == nil {
						ms = v
					}
				}
			}
		}
		result = append(result, stage{name: name, ms: ms})
	}
	return result
}
