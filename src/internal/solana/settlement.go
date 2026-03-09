package solana

import (
	"context"
	"fmt"
	"log"
	"strings"

	"opentela/internal/usage"
	"opentela/internal/wallet"

	"github.com/spf13/viper"
)

const (
	defaultMintAddress = "BAYyKYocgUgWHcs25Sar15x9XY9iYdtnKurBoVXJD1bU"
	defaultDecimals    = 9
)

// SubmitSettlement submits resolved usage records to Solana for payment.
// Returns a summary of transaction signatures or empty string if billing is disabled.
func SubmitSettlement(ctx context.Context, records []*usage.ResolvedUsage) (string, error) {
	if !viper.GetBool("billing.enabled") {
		return "", nil
	}

	if len(records) == 0 {
		return "", nil
	}

	// Initialize dependencies from config.
	rpcEndpoint := viper.GetString("solana.rpc_endpoint")
	client := NewClient(rpcEndpoint)

	mintAddr := viper.GetString("otela_token.mint_address")
	if mintAddr == "" {
		mintAddr = defaultMintAddress
	}
	decimals := viper.GetInt("otela_token.decimals")
	if decimals == 0 {
		decimals = defaultDecimals
	}

	defaultTokenRate := viper.GetInt64("rates.default_per_1000_tokens")
	defaultGPUMsRate := viper.GetInt64("rates.default_per_gpu_ms")
	rateManager := NewRateManager(defaultTokenRate, defaultGPUMsRate)

	// Load provider rates from config file if specified.
	ratesPath := viper.GetString("rates.config_path")
	if ratesPath != "" {
		if err := rateManager.LoadFromConfig(ratesPath); err != nil {
			log.Printf("settlement: failed to load rates config: %v", err)
		}
	}

	walletMgr, err := wallet.InitializeWallet()
	if err != nil {
		return "", fmt.Errorf("initialize wallet: %w", err)
	}

	processor, err := NewPaymentProcessor(client, rateManager, walletMgr, mintAddr, byte(decimals))
	if err != nil {
		return "", fmt.Errorf("create payment processor: %w", err)
	}

	// Verify consumer has sufficient balance before processing.
	consumerAddr := walletMgr.GetPublicKey()
	var totalEstimatedAmount int64
	for _, r := range records {
		if r.Disputed || r.HeadRecord == nil {
			continue
		}
		rate, err := rateManager.GetRate(r.HeadRecord.ProviderPeer, r.HeadRecord.Service, r.HeadRecord.MetricName)
		if err != nil {
			continue
		}
		totalEstimatedAmount += r.ResolvedValue * rate.PricePerUnit
	}

	if totalEstimatedAmount > 0 {
		hasFunds, err := processor.VerifyBalance(ctx, consumerAddr, totalEstimatedAmount)
		if err != nil {
			return "", fmt.Errorf("verify balance: %w", err)
		}
		if !hasFunds {
			return "", fmt.Errorf("insufficient OTELA balance for settlement (need ~%d base units)", totalEstimatedAmount)
		}
	}

	// Process payments.
	results, err := processor.ProcessUsageRecords(ctx, records)
	if err != nil {
		return "", fmt.Errorf("process payments: %w", err)
	}

	// Collect signatures.
	var signatures []string
	var errors []string
	for _, r := range results {
		if r.Success {
			signatures = append(signatures, r.Signature)
		} else if r.Error != nil {
			errors = append(errors, r.Error.Error())
		}
	}

	if len(errors) > 0 {
		log.Printf("settlement: %d/%d payments failed: %s", len(errors), len(results), strings.Join(errors, "; "))
	}

	if len(signatures) == 0 && len(errors) > 0 {
		return "", fmt.Errorf("all payments failed: %s", strings.Join(errors, "; "))
	}

	return strings.Join(signatures, ","), nil
}
