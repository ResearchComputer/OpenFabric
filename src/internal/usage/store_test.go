package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageStore_SaveAndGet(t *testing.T) {
	// Create temp directory
	tmpDir := filepath.Join(os.TempDir(), "usage-test-"+time.Now().Format("20060102150405"))
	defer os.RemoveAll(tmpDir)

	store, err := NewUsageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewUsageStore failed: %v", err)
	}
	defer store.Close()

	record := &UsageRecord{
		RequestID:    "test-req-1",
		Service:      "llm",
		ConsumerPeer: "head-1",
		ProviderPeer: "worker-1",
		MetricName:   "tokens",
		MetricValue:  1000,
		Timestamp:    time.Now().Unix(),
	}

	// Save
	if err := store.SaveRecord(record); err != nil {
		t.Fatalf("SaveRecord failed: %v", err)
	}

	// Get
	retrieved, err := store.GetRecord("test-req-1")
	if err != nil {
		t.Fatalf("GetRecord failed: %v", err)
	}

	if retrieved.MetricValue != 1000 {
		t.Errorf("Expected MetricValue=1000, got %d", retrieved.MetricValue)
	}
}

func TestUsageStore_GetPendingRecords(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "usage-test-"+time.Now().Format("20060102150405"))
	defer os.RemoveAll(tmpDir)

	store, err := NewUsageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewUsageStore failed: %v", err)
	}
	defer store.Close()

	// Save multiple records
	now := time.Now().Unix()
	for i := 0; i < 3; i++ {
		record := &UsageRecord{
			RequestID:    "test-req-" + string(rune('1'+i)),
			Service:      "llm",
			ConsumerPeer: "head-1",
			ProviderPeer: "worker-1",
			MetricName:   "tokens",
			MetricValue:  int64(1000 * (i + 1)),
			Timestamp:    now,
		}
		if err := store.SaveRecord(record); err != nil {
			t.Fatalf("SaveRecord failed: %v", err)
		}
	}

	// Get pending
	records, err := store.GetPendingRecords("head-1", "worker-1", "llm", "tokens")
	if err != nil {
		t.Fatalf("GetPendingRecords failed: %v", err)
	}

	if len(records) != 3 {
		t.Errorf("Expected 3 records, got %d", len(records))
	}
}

func TestUsageStore_MarkAggregated(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewUsageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewUsageStore failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()

	// Save 3 records
	for i := 0; i < 3; i++ {
		record := &UsageRecord{
			RequestID:    fmt.Sprintf("req-%d", i),
			Service:      "llm",
			ConsumerPeer: "head-1",
			ProviderPeer: "worker-1",
			MetricName:   "tokens",
			MetricValue:  int64(100 * (i + 1)),
			Timestamp:    now,
		}
		if err := store.SaveRecord(record); err != nil {
			t.Fatalf("SaveRecord failed: %v", err)
		}
	}

	// Mark 2 of the 3 as aggregated
	if err := store.MarkAggregated([]string{"req-0", "req-1"}); err != nil {
		t.Fatalf("MarkAggregated failed: %v", err)
	}

	// Verify only 1 remains pending
	records, err := store.GetPendingRecords("head-1", "worker-1", "llm", "tokens")
	if err != nil {
		t.Fatalf("GetPendingRecords failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 pending record, got %d", len(records))
	}

	if records[0].RequestID != "req-2" {
		t.Errorf("Expected remaining record to be req-2, got %s", records[0].RequestID)
	}

	if records[0].MetricValue != 300 {
		t.Errorf("Expected MetricValue=300, got %d", records[0].MetricValue)
	}
}

func TestUsageStore_MarkAggregated_EmptyList(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewUsageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewUsageStore failed: %v", err)
	}
	defer store.Close()

	// Marking an empty list should not error
	if err := store.MarkAggregated([]string{}); err != nil {
		t.Fatalf("MarkAggregated with empty list should not error, got: %v", err)
	}
}

func TestUsageStore_SaveAggregate(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewUsageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewUsageStore failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	agg := &AggregatedUsage{
		PeerID:      "peer-1",
		Service:     "llm",
		MetricName:  "tokens",
		TotalValue:  5000,
		RecordCount: 10,
		WindowStart: now - 3600,
		WindowEnd:   now,
	}

	// SaveAggregate should not return an error
	if err := store.SaveAggregate(agg); err != nil {
		t.Fatalf("SaveAggregate failed: %v", err)
	}
}

func TestUsageStore_GetRecord_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewUsageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewUsageStore failed: %v", err)
	}
	defer store.Close()

	// Request a non-existent record
	_, err = store.GetRecord("does-not-exist")
	if err == nil {
		t.Fatal("Expected error when getting non-existent record, got nil")
	}
}

func TestUsageStore_GetPendingRecords_Filtering(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewUsageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewUsageStore failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()

	// Save records with different consumer/provider/service/metric combinations
	records := []*UsageRecord{
		{RequestID: "r1", Service: "llm", ConsumerPeer: "head-1", ProviderPeer: "worker-1", MetricName: "tokens", MetricValue: 100, Timestamp: now},
		{RequestID: "r2", Service: "llm", ConsumerPeer: "head-1", ProviderPeer: "worker-1", MetricName: "tokens", MetricValue: 200, Timestamp: now},
		{RequestID: "r3", Service: "llm", ConsumerPeer: "head-2", ProviderPeer: "worker-1", MetricName: "tokens", MetricValue: 300, Timestamp: now},
		{RequestID: "r4", Service: "sandbox", ConsumerPeer: "head-1", ProviderPeer: "worker-1", MetricName: "tokens", MetricValue: 400, Timestamp: now},
		{RequestID: "r5", Service: "llm", ConsumerPeer: "head-1", ProviderPeer: "worker-2", MetricName: "tokens", MetricValue: 500, Timestamp: now},
		{RequestID: "r6", Service: "llm", ConsumerPeer: "head-1", ProviderPeer: "worker-1", MetricName: "gpu_ms", MetricValue: 600, Timestamp: now},
	}

	for _, rec := range records {
		if err := store.SaveRecord(rec); err != nil {
			t.Fatalf("SaveRecord failed for %s: %v", rec.RequestID, err)
		}
	}

	// Filter: head-1 + worker-1 + llm + tokens -> should match r1 and r2
	pending, err := store.GetPendingRecords("head-1", "worker-1", "llm", "tokens")
	if err != nil {
		t.Fatalf("GetPendingRecords failed: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("Expected 2 records for head-1/worker-1/llm/tokens, got %d", len(pending))
	}

	// Filter: head-2 + worker-1 + llm + tokens -> should match r3 only
	pending, err = store.GetPendingRecords("head-2", "worker-1", "llm", "tokens")
	if err != nil {
		t.Fatalf("GetPendingRecords failed: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("Expected 1 record for head-2/worker-1/llm/tokens, got %d", len(pending))
	}

	// Filter: head-1 + worker-1 + sandbox + tokens -> should match r4 only
	pending, err = store.GetPendingRecords("head-1", "worker-1", "sandbox", "tokens")
	if err != nil {
		t.Fatalf("GetPendingRecords failed: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("Expected 1 record for head-1/worker-1/sandbox/tokens, got %d", len(pending))
	}

	// Filter: head-1 + worker-2 + llm + tokens -> should match r5 only
	pending, err = store.GetPendingRecords("head-1", "worker-2", "llm", "tokens")
	if err != nil {
		t.Fatalf("GetPendingRecords failed: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("Expected 1 record for head-1/worker-2/llm/tokens, got %d", len(pending))
	}

	// Filter: head-1 + worker-1 + llm + gpu_ms -> should match r6 only
	pending, err = store.GetPendingRecords("head-1", "worker-1", "llm", "gpu_ms")
	if err != nil {
		t.Fatalf("GetPendingRecords failed: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("Expected 1 record for head-1/worker-1/llm/gpu_ms, got %d", len(pending))
	}

	// Filter: non-existent combination -> should return 0 records
	pending, err = store.GetPendingRecords("head-99", "worker-99", "unknown", "nothing")
	if err != nil {
		t.Fatalf("GetPendingRecords failed: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("Expected 0 records for non-existent filter, got %d", len(pending))
	}
}
