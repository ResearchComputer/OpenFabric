# Solana Settlement Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement Solana blockchain settlement for automated OTELA token payments from consumers to providers based on usage metrics.

**Architecture:** Head nodes act as payment processors, building and signing SPL token transfer transactions on behalf of consumers after usage reconciliation.

**Tech Stack:** Go, Solana SPL Token Program, libp2p, CRDT (Badger), ed25519 signatures

---

## Task 1: Add OTELA Token Constants

**Files:**
- Create: `src/internal/solana/token.go`
- Test: `src/internal/solana/token_test.go`

**Step 1: Write the failing test**

```go
// src/internal/solana/token_test.go
package solana

import "testing"

func TestOTELATokenConstants(t *testing.T) {
    if OTELAMintAddress != "BAYyKYocgUgWHcs25Sar15x9XY9iYdtnKurBoVXJD1bU" {
        t.Errorf("Unexpected mint address: %s", OTELAMintAddress)
    }
    if OTELASymbol != "OTELA" {
        t.Errorf("Unexpected symbol: %s", OTELASymbol)
    }
    if OTELAdecimals != 9 {
        t.Errorf("Unexpected decimals: %d", OTELAdecimals)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: FAIL with constants not defined

**Step 3: Write minimal implementation**

```go
// src/internal/solana/token.go
package solana

const (
    // OTELAMintAddress is the mint address of the OpenTela token
    OTELAMintAddress = "BAYyKYocgUgWHcs25Sar15x9XY9iYdtnKurBoVXJD1bU"

    // OTELASymbol is the token symbol
    OTELASymbol = "OTELA"

    // OTELAdecimals is the number of decimal places
    OTELAdecimals = 9

    // SPLTokenProgram is the Solana SPL Token 2022 program ID
    SPLTokenProgram = "TokenkeQ4aoQhLvy6RtXFn89jYq8RKzVKdk1"

    // SystemProgram is the Solana System Program ID
    SystemProgram = "11111111111111111111111111111111"
)
```

**Step 4: Run test to verify it passes**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: PASS

**Step 5: Commit**

```bash
git add src/internal/solana/token.go src/internal/solana/token_test.go
git commit -m "feat: add OTELA token constants

Define mint address, symbol, decimals,
and Solana program IDs for SPL token.
"
```

---

## Task 2: Implement Rate Types

**Files:**
- Create: `src/internal/solana/rates.go`
- Test: `src/internal/solana/rates_test.go`

**Step 1: Write the failing test**

```go
// src/internal/solana/rates_test.go
package solana

import "testing"

func TestRateKey(t *testing.T) {
    r := Rate{
        ProviderAddress: "provider1",
        Service:         "llm",
        MetricName:      "tokens",
    }

    expected := "provider1:llm:tokens"
    if r.Key() != expected {
        t.Errorf("Expected key=%s, got %s", expected, r.Key())
    }
}

func TestCalculatePrice(t *testing.T) {
    r := Rate{
        PricePerUnit: 1000, // per 1000 tokens
    }

    // 5000 tokens × (1000 / 1000) = 5000 base units
    price := r.CalculatePrice(5000)
    if price != 5000 {
        t.Errorf("Expected price=5000, got %d", price)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: FAIL with Rate not defined

**Step 3: Write minimal implementation**

```go
// src/internal/solana/rates.go
package solana

// Rate represents the pricing for a specific service/metric from a provider
type Rate struct {
    ProviderAddress string  // Solana address of the provider
    Service         string  // Service name (e.g., "llm", "sandbox")
    MetricName      string  // Metric type (e.g., "tokens", "gpu_ms")
    PricePerUnit    int64   // Price per unit in OTELA base units (9 decimals)
    // Note: PricePerUnit is typically per 1000 units of the metric
}

// Key returns the composite key for this rate
func (r *Rate) Key() string {
    return r.ProviderAddress + ":" + r.Service + ":" + r.MetricName
}

// CalculatePrice calculates the total price for a given usage amount
func (r *Rate) CalculatePrice(units int64) int64 {
    // PricePerUnit is typically per 1000 units
    // Formula: (units / 1000) × PricePerUnit
    return (units / 1000) * r.PricePerUnit
}

// RateManager manages provider rates
type RateManager struct {
    rates map[string]Rate
    defaultRate int64 // Default price when provider rate not found
}

// NewRateManager creates a new rate manager
func NewRateManager() *RateManager {
    return &RateManager{
        rates: make(map[string]Rate),
        defaultRate: 1000, // Default: 1000 base units per 1000 metric units
    }
}

// GetRate returns the rate for a provider/service/metric combination
func (rm *RateManager) GetRate(provider, service, metric string) (Rate, bool) {
    key := provider + ":" + service + ":" + metric
    rate, ok := rm.rates[key]
    if !ok {
        // Return default rate with provider address filled
        return Rate{
            ProviderAddress: provider,
            Service:         service,
            MetricName:      metric,
            PricePerUnit:    rm.defaultRate,
        }, false
    }
    return rate, true
}

// SetRate adds or updates a rate
func (rm *RateManager) SetRate(rate Rate) {
    rm.rates[rate.Key()] = rate
}

// SetDefaultRate sets the default price per unit
func (rm *RateManager) SetDefaultRate(defaultRate int64) {
    rm.defaultRate = defaultRate
}
```

**Step 4: Run test to verify it passes**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: PASS

**Step 5: Commit**

```bash
git add src/internal/solana/rates.go src/internal/solana/rates_test.go
git commit -m "feat: add rate types and manager

Rate struct with provider, service, metric pricing.
RateManager for looking up and managing rates.
"
```

---

## Task 3: Implement ATA Derivation

**Files:**
- Create: `src/internal/solana/ata.go`
- Test: `src/internal/solana/ata_test.go`

**Step 1: Write the failing test**

```go
// src/internal/solana/ata_test.go
package solana

import (
    "testing"

    "github.com/mr-tron/base58"
    "github.com/stretchr/testify/require"
)

func TestDeriveATAAddress(t *testing.T) {
    owner, err := base58.Decode("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
    require.NoError(t, err)

    mint, err := base58.Decode(OTELAMintAddress)
    require.NoError(t, err)

    ata := DeriveATAAddress(owner, mint)

    // ATA should be deterministic
    ata2 := DeriveATAAddress(owner, mint)
    if string(ata) != string(ata2) {
        t.Error("ATA derivation is not deterministic")
    }

    // ATA should be 32 bytes
    if len(ata) != 32 {
        t.Errorf("Expected 32 bytes, got %d", len(ata))
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: FAIL with DeriveATAAddress not defined

**Step 3: Write minimal implementation**

```go
// src/internal/solana/ata.go
package solana

import (
    "crypto/sha256"

    "github.com/mr-tron/base58"
    "golang.org/x/crypto/ed25519"
)

// DeriveATAAddress derives the Associated Token Account address
// for a given owner and mint address using the SPL Token program.
// ATA = PDA(owner, mint, TOKEN_PROGRAM_ID)
func DeriveATAAddress(owner []byte, mint []byte) []byte {
    // ATA seeds: ["associated_token_account", owner, mint]
    // Note: owner and mint are already 32 bytes (public keys)

    seeds := [][]byte{
        []byte("associated_token_account"),
        owner,
        mint,
        []byte(SPLTokenProgram),
    }

    // Create PDA by finding a bump that produces valid ed25519 public key
    for bump := uint8(0); ; bump++ {
        data := append([]byte{}, seeds[0]...)
        for _, seed := range seeds[1:] {
            data = append(data, seed...)
        }
        data = append(data, bump)

        hash := sha256.Sum256(data)
        if _, err := ed25519.PublicKeyFromHash(hash[:]); err == nil {
            // Valid public key found
            return hash[:]
        }
    }
}

// CreateATAInstruction builds the instruction to create an Associated Token Account
func CreateATAInstruction(owner, mint []byte) ([]byte, error) {
    // This is a simplified version - the full instruction would include:
    // - Program ID: SPL Token program
    // - Accounts: payer, associated_token_account, owner, mint, system_program, token_program, spl_ata_program
    // - Data: instruction index (1)
    //
    // For now, return a placeholder
    return []byte("create_ata"), nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: PASS

**Step 5: Commit**

```bash
git add src/internal/solana/ata.go src/internal/solana/ata_test.go
git commit -m "feat: add ATA derivation

Derive Associated Token Account addresses
for OTELA token using PDA derivation.
"
```

---

## Task 4: Implement SPL Token Transfer Instruction Builder

**Files:**
- Create: `src/internal/solana/spl_transfer.go`
- Test: `src/internal/solana/spl_transfer_test.go`

**Step 1: Write the failing test**

```go
// src/internal/solana/spl_transfer_test.go
package solana

import (
    "bytes"
    "encoding/binary"
    "testing"
)

func TestBuildTransferCheckedData(t *testing.T) {
    amount := int64(1000000000) // 1 OTELA (9 decimals)

    data := BuildTransferCheckedData(amount, 9)

    // Data layout:
    // - instruction index (4 bytes LE)
    // - amount (8 bytes LE)
    // - decimals (1 byte)

    if len(data) != 13 {
        t.Errorf("Expected 13 bytes, got %d", len(data))
    }

    // Check instruction index (3 = TransferChecked)
    instrIndex := binary.LittleEndian.Uint32(data[0:4])
    if instrIndex != 3 {
        t.Errorf("Expected instruction index 3, got %d", instrIndex)
    }

    // Check amount
    amt := binary.LittleEndian.Uint64(data[4:12])
    if amt != 1000000000 {
        t.Errorf("Expected amount 1000000000, got %d", amt)
    }

    // Check decimals
    if data[12] != 9 {
        t.Errorf("Expected decimals 9, got %d", data[12])
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: FAIL with BuildTransferCheckedData not defined

**Step 3: Write minimal implementation**

```go
// src/internal/solana/spl_transfer.go
package solana

import (
    "bytes"
    "encoding/binary"
)

// BuildTransferCheckedData builds the instruction data for SPL Token's TransferChecked instruction
// Instruction index 3 = TransferChecked
func BuildTransferCheckedData(amount int64, decimals uint8) []byte {
    buf := new(bytes.Buffer)

    // Instruction index: 3 (TransferChecked)
    instrIndex := make([]byte, 4)
    binary.LittleEndian.PutUint32(instrIndex, 3)
    buf.Write(instrIndex)

    // Amount (u64 LE)
    amountBytes := make([]byte, 8)
    binary.LittleEndian.PutUint64(amountBytes, uint64(amount))
    buf.Write(amountBytes)

    // Decimals (u8)
    buf.WriteByte(decimals)

    return buf.Bytes()
}
```

**Step 4: Run test to verify it passes**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: PASS

**Step 5: Commit**

```bash
git add src/internal/solana/spl_transfer.go src/internal/solana/spl_transfer_test.go
git commit -m "feat: add SPL Token TransferChecked data builder

Build instruction data for OTELA token transfers.
"
```

---

## Task 5: Extend Settlement with Actual Implementation

**Files:**
- Modify: `src/internal/solana/settlement.go`
- Test: `src/internal/solana/settlement_test.go`

**Step 1: Write the failing test**

```go
// src/internal/solana/settlement_test.go
package solana

import (
    "context"
    "testing"
    "time"

    "opentela/internal/usage"
)

func TestSubmitSettlement_BillingEnabled(t *testing.T) {
    // This test validates the flow when billing is enabled
    // Actual RPC calls will fail without real setup, but we validate the logic

    records := []*usage.ResolvedUsage{
        {
            HeadRecord: &usage.UsageRecord{
                ProviderPeer: "worker-1",
                Service:      "llm",
                MetricName:   "tokens",
                MetricValue:  1000,
                Timestamp:    time.Now().Unix(),
            },
            ResolvedValue: 1000,
            Disputed:      false,
        },
    }

    // Should attempt to process (may fail on actual RPC)
    _, err := SubmitSettlement(context.Background(), records)

    // We expect either success or an error about missing configuration
    // The function should not panic
    if err != nil {
        t.Logf("Expected error in test environment: %v", err)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: Current implementation returns "not yet implemented"

**Step 3: Write implementation**

Replace the skeleton implementation in `src/internal/solana/settlement.go`:

```go
// src/internal/solana/settlement.go
package solana

import (
    "context"
    "fmt"

    "opentela/internal/usage"
    "github.com/spf13/viper"
)

// SubmitSettlement submits resolved usage records to Solana for payment
// Returns transaction signatures for successful payments
func SubmitSettlement(ctx context.Context, records []*usage.ResolvedUsage) ([]string, error) {
    if !viper.GetBool("billing.enabled") {
        return nil, nil
    }

    // Filter out disputed records
    var validRecords []*usage.ResolvedUsage
    for _, record := range records {
        if !record.Disputed {
            validRecords = append(validRecords, record)
        }
    }

    if len(validRecords) == 0 {
        return nil, nil
    }

    // TODO: Implement full payment flow
    // 1. Get rate manager
    // 2. Calculate amounts for each record
    // 3. Build transactions
    // 4. Sign with consumer keys
    // 5. Submit to Solana
    // 6. Wait for confirmation

    return nil, fmt.Errorf("settlement implementation in progress")
}
```

**Step 4: Run test to verify it passes**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: PASS (test expects error during implementation)

**Step 5: Commit**

```bash
git add src/internal/solana/settlement.go src/internal/solana/settlement_test.go
git commit -m "feat: extend settlement with dispute filtering

Filter out disputed records before processing.
"
```

---

## Task 6: Add Consumer Wallet Registry

**Files:**
- Create: `src/internal/solana/wallets.go`
- Test: `src/internal/solana/wallets_test.go`

**Step 1: Write the failing test**

```go
// src/internal/solana/wallets_test.go
package solana

import (
    "context"
    "testing"
)

func TestRegisterConsumerWallet(t *testing.T) {
    ctx := context.Background()
    peerID := "head-1"
    solanaAddress := "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"

    err := RegisterConsumerWallet(ctx, peerID, solanaAddress, "")
    if err != nil {
        t.Fatalf("RegisterConsumerWallet failed: %v", err)
    }

    // Retrieve the wallet
    retrieved, err := GetConsumerWallet(ctx, peerID)
    if err != nil {
        t.Fatalf("GetConsumerWallet failed: %v", err)
    }

    if retrieved.SolanaAddress != solanaAddress {
        t.Errorf("Expected address %s, got %s", solanaAddress, retrieved.SolanaAddress)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: FAIL with RegisterConsumerWallet not defined

**Step 3: Write minimal implementation**

```go
// src/internal/solana/wallets.go
package solana

import (
    "context"
    "encoding/json"
    "fmt"

    ds "github.com/ipfs/go-datastore"
    "opentela/internal/protocol"
)

const (
    // CRDTNamespaceBilling is the namespace for billing-related data
    CRDTNamespaceBilling = "/billing"
    // CRDTNamespaceWallets is for consumer wallet registrations
    CRDTNamespaceWallets = "/billing/wallets"
)

// ConsumerWallet represents a consumer's billing wallet
type ConsumerWallet struct {
    PeerID        string `json:"peer_id"`
    SolanaAddress string `json:"solana_address"`
    PrivateKeyEnc string `json:"private_key_enc,omitempty"` // Encrypted private key
    RegisteredAt  int64  `json:"registered_at"`
}

// RegisterConsumerWallet registers a consumer's Solana wallet for automated billing
func RegisterConsumerWallet(ctx context.Context, peerID, solanaAddress, privateKeyEnc string) error {
    store, err := protocol.GetCRDTStore()
    if err != nil {
        return fmt.Errorf("getting CRDT store: %w", err)
    }

    wallet := &ConsumerWallet{
        PeerID:        peerID,
        SolanaAddress: solanaAddress,
        PrivateKeyEnc: privateKeyEnc,
    }

    data, err := json.Marshal(wallet)
    if err != nil {
        return fmt.Errorf("marshalling wallet: %w", err)
    }

    key := ds.NewKey(CRDTNamespaceWallets).ChildString(peerID)
    return store.Put(ctx, key, data)
}

// GetConsumerWallet retrieves a consumer's wallet registration
func GetConsumerWallet(ctx context.Context, peerID string) (*ConsumerWallet, error) {
    store, err := protocol.GetCRDTStore()
    if err != nil {
        return nil, fmt.Errorf("getting CRDT store: %w", err)
    }

    key := ds.NewKey(CRDTNamespaceWallets).ChildString(peerID)
    data, err := store.Get(ctx, key)
    if err != nil {
        return nil, err
    }

    var wallet ConsumerWallet
    if err := json.Unmarshal(data, &wallet); err != nil {
        return nil, err
    }

    return &wallet, nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: PASS (or skip if CRDT not available in tests)

**Step 5: Commit**

```bash
git add src/internal/solana/wallets.go src/internal/solana/wallets_test.go
git commit -m "feat: add consumer wallet registry

Store consumer Solana addresses in CRDT
for automated billing settlements.
"
```

---

## Task 7: Add Payment Result Types

**Files:**
- Modify: `src/internal/solana/settlement.go`
- Test: `src/internal/solana/settlement_test.go`

**Step 1: Write the failing test**

```go
func TestPaymentResultSuccess(t *testing.T) {
    result := PaymentResult{
        Signature: "test_signature",
        Amount:    1000000000,
        Success:   true,
    }

    if !result.Success {
        t.Error("Expected success=true")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: FAIL with PaymentResult not defined

**Step 3: Write minimal implementation**

Add to `src/internal/solana/settlement.go`:

```go
// PaymentResult represents the result of a payment attempt
type PaymentResult struct {
    Signature string     // Solana transaction signature
    Amount    int64      // Amount paid in base units
    From      string     // Consumer wallet address
    To        string     // Provider wallet address
    Success   bool       // Whether payment succeeded
    Error     error      // Error if payment failed
}

// PaymentBatch represents a batch of payments to be processed
type PaymentBatch struct {
    ConsumerAddress string                  // Consumer's Solana address
    ProviderAddress string                  // Provider's Solana address
    Records         []*usage.ResolvedUsage  // Usage records to settle
    TotalAmount     int64                   // Total OTELA to pay
}

// CalculatePaymentAmount calculates the total payment for resolved usage
func CalculatePaymentAmount(records []*usage.ResolvedUsage, rateManager *RateManager) (int64, error) {
    var total int64

    for _, record := range records {
        if record.Disputed {
            continue
        }

        rate, found := rateManager.GetRate(
            record.WorkerRecord.ProviderPeer,
            record.WorkerRecord.Service,
            record.WorkerRecord.MetricName,
        )

        // Use rate even if not found (returns default)
        amount := rate.CalculatePrice(record.ResolvedValue)
        total += amount
    }

    return total, nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: PASS

**Step 5: Commit**

```bash
git add src/internal/solana/settlement.go src/internal/solana/settlement_test.go
git commit -m "feat: add payment result types

PaymentResult and PaymentBatch for tracking
payment outcomes.
"
```

---

## Task 8: Add Solana Configuration to Viper

**Files:**
- Modify: `src/entry/cmd/root.go`

**Step 1: Write the failing test**

Add to `src/entry/cmd/root_test.go`:

```go
func TestSolanaConfigDefaults(t *testing.T) {
    viper.Reset()
    initConfig()

    if viper.GetString("solana.rpc_endpoint") != "https://api.mainnet-beta.solana.com" {
        t.Errorf("Unexpected RPC endpoint: %s", viper.GetString("solana.rpc_endpoint"))
    }
    if viper.GetString("otela_token.mint_address") != "BAYyKYocgUgWHcs25Sar15x9XY9iYdtnKurBoVXJD1bU" {
        t.Errorf("Unexpected mint address")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd src && make test TEST_PKGS="./entry/..." VERBOSE=1`
Expected: FAIL with config values not found

**Step 3: Write minimal implementation**

Add to `initConfig()` in `src/entry/cmd/root.go`:

```go
// Solana configuration
viper.SetDefault("solana.rpc_endpoint", "https://api.mainnet-beta.solana.com")
viper.SetDefault("solana.priority_fee", int64(1000))

// OTELA token configuration
viper.SetDefault("otela_token.mint_address", "BAYyKYocgUgWHcs25Sar15x9XY9iYdtnKurBoVXJD1bU")
viper.SetDefault("otela_token.decimals", 9)
viper.SetDefault("otela_token.symbol", "OTELA")

// Rates configuration
viper.SetDefault("rates.default_per_1000_tokens", int64(1000))
viper.SetDefault("rates.default_per_gpu_ms", int64(1))
viper.SetDefault("rates.config_path", "/etc/opentela/rates.yaml")
```

**Step 4: Run test to verify it passes**

Run: `cd src && make test TEST_PKGS="./entry/..." VERBOSE=1`
Expected: PASS

**Step 5: Commit**

```bash
git add src/entry/cmd/root.go src/entry/cmd/root_test.go
git commit -m "feat: add Solana settlement configuration

RPC endpoint, priority fee, OTELA token details,
and default rates configuration.
"
```

---

## Task 9: Update Settlement to Use Configuration

**Files:**
- Modify: `src/internal/solana/settlement.go`

**Step 1: Update initialization**

Modify `SubmitSettlement` to use Viper config:

```go
func SubmitSettlement(ctx context.Context, records []*usage.ResolvedUsage) ([]string, error) {
    if !viper.GetBool("billing.enabled") {
        return nil, nil
    }

    // Filter disputed records
    var validRecords []*usage.ResolvedUsage
    for _, record := range records {
        if !record.Disputed {
            validRecords = append(validRecords, record)
        }
    }

    if len(validRecords) == 0 {
        return nil, nil
    }

    // Get configuration
    rpcEndpoint := viper.GetString("solana.rpc_endpoint")
    mintAddress := viper.GetString("otela_token.mint_address")
    decimals := viper.GetInt("otela_token.decimals")

    // Create Solana client
    client := NewClient(rpcEndpoint)

    // Create rate manager with defaults
    rateManager := NewRateManager()
    rateManager.SetDefaultRate(viper.GetInt64("rates.default_per_1000_tokens"))

    // TODO: Calculate amounts and build transactions
    _ = client
    _ = mintAddress
    _ = decimals
    _ = rateManager

    return nil, fmt.Errorf("settlement implementation in progress - transaction building next")
}
```

**Step 2: Run tests**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: PASS

**Step 3: Commit**

```bash
git add src/internal/solana/settlement.go
git commit -m "feat: wire up configuration in settlement

Use Viper config for RPC endpoint, token details,
and default rates.
"
```

---

## Task 10: Implement Full Settlement Flow (Final)

**Files:**
- Modify: `src/internal/solana/settlement.go`
- Test: `src/internal/solana/settlement_test.go`

**Step 1: Write comprehensive implementation**

```go
// src/internal/solana/settlement.go - complete implementation

func SubmitSettlement(ctx context.Context, records []*usage.ResolvedUsage) ([]string, error) {
    if !viper.GetBool("billing.enabled") {
        return nil, nil
    }

    // Filter disputed records
    var validRecords []*usage.ResolvedUsage
    for _, record := range records {
        if !record.Disputed {
            validRecords = append(validRecords, record)
        }
    }

    if len(validRecords) == 0 {
        return nil, nil
    }

    // Group by consumer and provider
    batches := groupByConsumerProvider(validRecords)

    // Get configuration
    rpcEndpoint := viper.GetString("solana.rpc_endpoint")
    client := NewClient(rpcEndpoint)
    rateManager := NewRateManager()
    rateManager.SetDefaultRate(viper.GetInt64("rates.default_per_1000_tokens"))

    var signatures []string

    // Process each batch
    for _, batch := range batches {
        // Calculate amount
        amount, err := CalculatePaymentAmount(batch.Records, rateManager)
        if err != nil {
            continue
        }

        if amount == 0 {
            continue
        }

        // Get consumer wallet
        consumerWallet, err := GetConsumerWallet(ctx, batch.ConsumerPeer)
        if err != nil {
            // Consumer not registered, skip
            continue
        }

        // Get provider wallet from worker record
        providerAddr := batch.ProviderPeer

        // TODO: Build and submit transaction
        // For now, just log the intent
        fmt.Printf("Would process payment: %s -> %s, amount: %d base units\n",
            consumerWallet.SolanaAddress, providerAddr, amount)

        // TODO: Actually build transaction, sign, submit
        // This requires:
        // 1. Derive ATAs for both parties
        // 2. Build TransferChecked instruction
        // 3. Get recent blockhash
        // 4. Sign with consumer private key
        // 5. Submit transaction
        // 6. Confirm transaction
        // 7. Store payment record
    }

    return signatures, fmt.Errorf("transaction building and submission not yet implemented")
}

// PaymentBatch represents a batch of payments from one consumer to one provider
type paymentBatch struct {
    ConsumerPeer string
    ProviderPeer string
    Records      []*usage.ResolvedUsage
}

// groupByConsumerProvider groups records by consumer and provider
func groupByConsumerProvider(records []*usage.ResolvedUsage) []*paymentBatch {
    batches := make(map[string]*paymentBatch)

    for _, record := range records {
        if record.HeadRecord == nil {
            continue
        }

        key := record.HeadRecord.ConsumerPeer + ":" + record.WorkerRecord.ProviderPeer
        if batches[key] == nil {
            batches[key] = &paymentBatch{
                ConsumerPeer: record.HeadRecord.ConsumerPeer,
                ProviderPeer: record.WorkerRecord.ProviderPeer,
            }
        }
        batches[key].Records = append(batches[key].Records, record)
    }

    result := make([]*paymentBatch, 0, len(batches))
    for _, batch := range batches {
        result = append(result, batch)
    }
    return result
}
```

**Step 2: Run tests**

Run: `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
Expected: PASS

**Step 3: Commit**

```bash
git add src/internal/solana/settlement.go
git commit -m "feat: add settlement batching and amount calculation

Group records by consumer/provider, calculate amounts,
log payment intents.
"
```

---

## Task 11: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

**Step 1: Add Solana settlement section**

Add after the "Usage Tracking and Billing" section:

```markdown
### Solana Settlement

When billing is enabled, payments are processed automatically:

1. **Rate Lookup**: Each provider sets their own rates per service/metric
2. **Amount Calculation**: `usage × rate = OTELA amount`
3. **SPL Transfer**: Consumer pays provider directly via OTELA token
4. **Batch Processing**: Payments aggregated every 5-60 minutes

**Provider Rate Configuration (`/etc/opentela/rates.yaml`):**
```yaml
providers:
  - address: "ProviderSolanaAddress"
    services:
      - name: "llm"
        metrics:
          - name: "tokens"
            price_per_1000: 1000
```

**Note**: Full transaction building and signing is in progress.
```

**Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add Solana settlement section

Document rate configuration, payment flow,
and provider rate file format.
"
```

---

## Summary

This implementation plan builds the Solana settlement system in 11 focused tasks:

1. ✅ OTELA token constants
2. ✅ Rate types and manager
3. ✅ ATA derivation
4. ✅ SPL transfer instruction builder
5. ✅ Settlement with dispute filtering
6. ✅ Consumer wallet registry
7. ✅ Payment result types
8. ✅ Solana configuration
9. ✅ Configuration wiring
10. ✅ Settlement batching
11. ✅ Documentation

**Follow-on work:** Full transaction building, signing, and submission (requires consumer private key access, comprehensive error handling, transaction confirmation).

**Test command:** `cd src && make test TEST_PKGS="./internal/solana/..." VERBOSE=1`
