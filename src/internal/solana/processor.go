package solana

import (
	"context"
	"fmt"
	"log"
	"time"

	"opentela/internal/usage"
	"opentela/internal/wallet"

	"github.com/mr-tron/base58"
)

const (
	// confirmTimeout is how long to wait for transaction confirmation.
	confirmTimeout = 60 * time.Second
	// confirmPollInterval is how often to poll for confirmation status.
	confirmPollInterval = 2 * time.Second
	// maxRetries is the number of retry attempts for transient failures.
	maxRetries = 3
)

// PaymentResult represents the outcome of a single settlement payment.
type PaymentResult struct {
	Signature string
	Amount    int64
	From      string // consumer wallet address
	To        string // provider wallet address
	Success   bool
	Error     error
}

// PaymentProcessor orchestrates the settlement payment workflow.
type PaymentProcessor struct {
	client      *Client
	rateManager *RateManager
	walletMgr   *wallet.WalletManager
	mintAddress  []byte
	mintBase58   string
	decimals     byte
}

// NewPaymentProcessor creates a PaymentProcessor with the given dependencies.
func NewPaymentProcessor(
	client *Client,
	rateManager *RateManager,
	walletMgr *wallet.WalletManager,
	mintAddress string,
	decimals byte,
) (*PaymentProcessor, error) {
	mintBytes, err := base58.Decode(mintAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid mint address: %w", err)
	}
	if len(mintBytes) != 32 {
		return nil, fmt.Errorf("mint address must be 32 bytes, got %d", len(mintBytes))
	}

	return &PaymentProcessor{
		client:      client,
		rateManager: rateManager,
		walletMgr:   walletMgr,
		mintAddress:  mintBytes,
		mintBase58:   mintAddress,
		decimals:     decimals,
	}, nil
}

// ProcessUsageRecords processes a batch of resolved usage records,
// calculating amounts and submitting payments. Disputed records are skipped.
func (pp *PaymentProcessor) ProcessUsageRecords(ctx context.Context, records []*usage.ResolvedUsage) ([]PaymentResult, error) {
	// Group non-disputed records by provider.
	type paymentGroup struct {
		providerAddress string
		service         string
		metricName      string
		totalValue      int64
	}

	groups := make(map[string]*paymentGroup)

	for _, r := range records {
		if r.Disputed {
			log.Printf("settlement: skipping disputed record (head=%s, worker=%s)",
				r.HeadRecord.ConsumerPeer, r.WorkerRecord.ProviderPeer)
			continue
		}

		if r.HeadRecord == nil {
			continue
		}

		key := r.HeadRecord.ProviderPeer + ":" + r.HeadRecord.Service + ":" + r.HeadRecord.MetricName
		g, ok := groups[key]
		if !ok {
			g = &paymentGroup{
				providerAddress: r.HeadRecord.ProviderPeer,
				service:         r.HeadRecord.Service,
				metricName:      r.HeadRecord.MetricName,
			}
			groups[key] = g
		}
		g.totalValue += r.ResolvedValue
	}

	var results []PaymentResult

	for _, g := range groups {
		rate, err := pp.rateManager.GetRate(g.providerAddress, g.service, g.metricName)
		if err != nil {
			log.Printf("settlement: rate lookup failed for %s/%s/%s: %v",
				g.providerAddress, g.service, g.metricName, err)
			results = append(results, PaymentResult{
				To:    g.providerAddress,
				Error: fmt.Errorf("rate lookup: %w", err),
			})
			continue
		}

		amount := g.totalValue * rate.PricePerUnit
		if amount <= 0 {
			continue
		}

		result := pp.submitPayment(ctx, g.providerAddress, amount)
		results = append(results, result)
	}

	return results, nil
}

// submitPayment handles a single payment to a provider with retries.
func (pp *PaymentProcessor) submitPayment(ctx context.Context, providerAddress string, amount int64) PaymentResult {
	result := PaymentResult{
		Amount: amount,
		To:     providerAddress,
	}

	// Get consumer (head node) wallet.
	consumerKey, err := pp.walletMgr.GetPrivateKeyBytes()
	if err != nil {
		result.Error = fmt.Errorf("get consumer wallet: %w", err)
		return result
	}
	result.From = pp.walletMgr.GetPublicKey()

	// Derive ATAs.
	consumerPubBytes, err := base58.Decode(result.From)
	if err != nil {
		result.Error = fmt.Errorf("decode consumer pubkey: %w", err)
		return result
	}

	providerPubBytes, err := base58.Decode(providerAddress)
	if err != nil {
		result.Error = fmt.Errorf("decode provider pubkey: %w", err)
		return result
	}

	fromATA, err := FindATA(consumerPubBytes, pp.mintAddress)
	if err != nil {
		result.Error = fmt.Errorf("derive consumer ATA: %w", err)
		return result
	}

	toATA, err := FindATA(providerPubBytes, pp.mintAddress)
	if err != nil {
		result.Error = fmt.Errorf("derive provider ATA: %w", err)
		return result
	}

	// Check if provider ATA exists; create it if not.
	providerHasToken, err := pp.client.HasSPLToken(ctx, providerAddress, pp.mintBase58)
	if err != nil {
		result.Error = fmt.Errorf("check provider token account: %w", err)
		return result
	}
	if !providerHasToken {
		log.Printf("settlement: creating ATA for provider %s", providerAddress)
		_, err := pp.client.CreateATA(ctx, consumerKey, providerPubBytes, pp.mintAddress)
		if err != nil {
			result.Error = fmt.Errorf("create provider ATA: %w", err)
			return result
		}
	}

	// Submit transfer with retries.
	var sig string
	for attempt := 1; attempt <= maxRetries; attempt++ {
		sig, err = pp.client.SendSPLTransfer(
			ctx, consumerKey,
			fromATA, toATA, pp.mintAddress,
			uint64(amount), pp.decimals,
		)
		if err == nil {
			break
		}
		log.Printf("settlement: transfer attempt %d/%d failed: %v", attempt, maxRetries, err)
		if attempt < maxRetries {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
	}
	if err != nil {
		result.Error = fmt.Errorf("transfer failed after %d attempts: %w", maxRetries, err)
		return result
	}

	result.Signature = sig

	// Wait for confirmation.
	if err := pp.confirmTransaction(ctx, sig); err != nil {
		result.Error = fmt.Errorf("confirmation failed: %w", err)
		return result
	}

	result.Success = true
	log.Printf("settlement: payment confirmed sig=%s amount=%d to=%s", sig, amount, providerAddress)
	return result
}

// confirmTransaction polls for transaction confirmation up to the timeout.
func (pp *PaymentProcessor) confirmTransaction(ctx context.Context, signature string) error {
	deadline := time.Now().Add(confirmTimeout)

	for time.Now().Before(deadline) {
		confirmed, err := pp.client.GetSignatureStatus(ctx, signature)
		if err != nil {
			return err
		}
		if confirmed {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(confirmPollInterval):
		}
	}

	return fmt.Errorf("transaction %s not confirmed within %v", signature, confirmTimeout)
}

// VerifyBalance checks whether the given address has at least `requiredAmount`
// of the OTELA token.
func (pp *PaymentProcessor) VerifyBalance(ctx context.Context, address string, requiredAmount int64) (bool, error) {
	rawAmount, _, err := pp.client.GetTokenBalance(ctx, address, pp.mintBase58)
	if err != nil {
		return false, err
	}

	// Parse the raw amount string.
	var balance int64
	if _, err := fmt.Sscanf(rawAmount, "%d", &balance); err != nil {
		return false, fmt.Errorf("parse token balance: %w", err)
	}

	return balance >= requiredAmount, nil
}
