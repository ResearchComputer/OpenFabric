package solana

import (
	"context"
	"testing"

	"opentela/internal/usage"

	"github.com/spf13/viper"
)

func TestSubmitSettlement_BillingDisabled(t *testing.T) {
	ctx := context.Background()
	records := []*usage.ResolvedUsage{
		{
			ResolvedValue: 1000,
			Disputed:      false,
		},
	}

	sig, err := SubmitSettlement(ctx, records)
	if err != nil {
		t.Fatalf("SubmitSettlement failed: %v", err)
	}

	if sig != "" {
		t.Errorf("Expected empty signature when billing disabled, got %s", sig)
	}
}

func TestSubmitSettlement_EmptyRecords(t *testing.T) {
	// Save and restore viper state
	oldVal := viper.GetBool("billing.enabled")
	defer viper.Set("billing.enabled", oldVal)

	viper.Set("billing.enabled", true)

	ctx := context.Background()
	records := []*usage.ResolvedUsage{}

	sig, err := SubmitSettlement(ctx, records)
	if err != nil {
		t.Fatalf("SubmitSettlement with empty records failed: %v", err)
	}
	if sig != "" {
		t.Errorf("Expected empty signature for empty records, got %s", sig)
	}
}

func TestSubmitSettlement_BillingDisabled_NilRecords(t *testing.T) {
	// Save and restore viper state
	oldVal := viper.GetBool("billing.enabled")
	defer viper.Set("billing.enabled", oldVal)

	viper.Set("billing.enabled", false)

	ctx := context.Background()

	sig, err := SubmitSettlement(ctx, nil)
	if err != nil {
		t.Fatalf("SubmitSettlement with nil records failed: %v", err)
	}
	if sig != "" {
		t.Errorf("Expected empty signature when billing disabled with nil records, got %s", sig)
	}
}
