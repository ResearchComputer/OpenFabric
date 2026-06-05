package server

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStageTimer_DisabledIsNoOp(t *testing.T) {
	timingEnabled = false
	s := newStageTimer()
	s.Mark("recv")
	s.Mark("dnt")
	s.AddRaw("worker_sglang_ttft", 220.0)
	assert.Equal(t, "", s.Header())
}

func TestStageTimer_EnabledRecordsStages(t *testing.T) {
	timingEnabled = true
	defer func() { timingEnabled = false }()
	s := newStageTimer()
	time.Sleep(2 * time.Millisecond)
	s.Mark("recv")
	time.Sleep(1 * time.Millisecond)
	s.Mark("dnt")
	h := s.Header()
	assert.True(t, strings.HasPrefix(h, "recv;dur=") || strings.Contains(h, ", recv;dur="),
		"expected recv stage in header: %q", h)
	assert.Contains(t, h, "dnt;dur=")
}

func TestStageTimer_AddRawAppends(t *testing.T) {
	timingEnabled = true
	defer func() { timingEnabled = false }()
	s := newStageTimer()
	s.AddRaw("worker_local_proxy", 1.1)
	s.AddRaw("worker_sglang_ttft", 220.0)
	h := s.Header()
	assert.Contains(t, h, "worker_local_proxy;dur=1.100")
	assert.Contains(t, h, "worker_sglang_ttft;dur=220.000")
}

func TestParseServerTiming_Canonical(t *testing.T) {
	stages := parseServerTiming("recv;dur=1.5, dnt;dur=0.3, peer_select;dur=0.05")
	assert.Equal(t, 3, len(stages))
	assert.Equal(t, "recv", stages[0].name)
	assert.InDelta(t, 1.5, stages[0].ms, 0.001)
	assert.Equal(t, "dnt", stages[1].name)
	assert.InDelta(t, 0.3, stages[1].ms, 0.001)
}

func TestParseServerTiming_Malformed(t *testing.T) {
	stages := parseServerTiming("garbage")
	assert.Equal(t, 1, len(stages))
	assert.Equal(t, "garbage", stages[0].name)
	assert.Equal(t, 0.0, stages[0].ms)

	assert.Nil(t, parseServerTiming(""))

	stages = parseServerTiming("recv;dur=notanumber, dnt;dur=0.5")
	assert.Equal(t, 2, len(stages))
	assert.Equal(t, 0.0, stages[0].ms)
	assert.InDelta(t, 0.5, stages[1].ms, 0.001)
}
