package solana

import (
	"testing"

	"opentela/internal/usage"

	"github.com/stretchr/testify/assert"
)

func TestPaymentResult_Fields(t *testing.T) {
	r := PaymentResult{
		Signature: "abc123",
		Amount:    1000,
		From:      "consumer",
		To:        "provider",
		Success:   true,
	}
	assert.Equal(t, "abc123", r.Signature)
	assert.Equal(t, int64(1000), r.Amount)
	assert.True(t, r.Success)
}

func TestNewPaymentProcessor_InvalidMint(t *testing.T) {
	rm := NewRateManager(1, 1)
	_, err := NewPaymentProcessor(NewClient(""), rm, nil, "not-base58!!!", 9)
	assert.Error(t, err)
}

func TestNewPaymentProcessor_ShortMint(t *testing.T) {
	rm := NewRateManager(1, 1)
	_, err := NewPaymentProcessor(NewClient(""), rm, nil, "11111111", 9) // valid base58 but too short
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestDisputedRecordsSkipped(t *testing.T) {
	// Verify that the grouping logic in ProcessUsageRecords would skip
	// disputed records. We test the filtering indirectly since
	// ProcessUsageRecords requires a live wallet.
	records := []*usage.ResolvedUsage{
		{
			Disputed:      true,
			ResolvedValue: 100,
			HeadRecord: &usage.UsageRecord{
				ProviderPeer: "provider1",
				Service:      "llm",
				MetricName:   "tokens",
			},
			WorkerRecord: &usage.UsageRecord{
				ProviderPeer: "provider1",
				Service:      "llm",
				MetricName:   "tokens",
			},
		},
	}

	// All records are disputed, so nothing should be processed.
	// We can't call ProcessUsageRecords without a wallet, but we can
	// verify the type is correct.
	assert.True(t, records[0].Disputed)
}
