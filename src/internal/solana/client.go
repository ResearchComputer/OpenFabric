package solana

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/mr-tron/base58"
)

const (
	defaultRPCEndpoint = "https://api.mainnet-beta.solana.com"
	lamportsPerSOL     = 1_000_000_000
)

type Client struct {
	endpoint   string
	httpClient *http.Client
}

func NewClient(endpoint string) *Client {
	if endpoint == "" {
		endpoint = defaultRPCEndpoint
	}
	return &Client{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// ---------------------------------------------------------------------------
// Generic JSON-RPC helper
// ---------------------------------------------------------------------------

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) call(ctx context.Context, method string, params []any, dest any) error {
	payload := rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to query Solana RPC: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("solana rpc returned status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("failed to decode Solana RPC response: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// HasSPLToken – checks whether `owner` holds a positive balance of `mint`
// ---------------------------------------------------------------------------

func (c *Client) HasSPLToken(ctx context.Context, owner string, mint string) (bool, error) {
	if _, err := base58.Decode(owner); err != nil {
		return false, fmt.Errorf("invalid owner public key: %w", err)
	}
	if _, err := base58.Decode(mint); err != nil {
		return false, fmt.Errorf("invalid mint address: %w", err)
	}

	params := []any{
		owner,
		map[string]string{"mint": mint},
		map[string]any{"encoding": "jsonParsed"},
	}

	var rpcResp tokenAccountsResponse
	if err := c.call(ctx, "getTokenAccountsByOwner", params, &rpcResp); err != nil {
		return false, err
	}

	if rpcResp.Error != nil {
		return false, fmt.Errorf("solana rpc error (%d): %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	for _, entry := range rpcResp.Result.Value {
		amount := entry.Account.Data.Parsed.Info.TokenAmount.Amount
		if amount == "" {
			continue
		}
		if i, ok := new(big.Int).SetString(amount, 10); ok && i.Sign() > 0 {
			return true, nil
		}
	}

	return false, nil
}

// ---------------------------------------------------------------------------
// GetBalance – returns the SOL balance in lamports for the given pubkey
// ---------------------------------------------------------------------------

type balanceResponse struct {
	Result struct {
		Value uint64 `json:"value"`
	} `json:"result"`
	Error *rpcError `json:"error"`
}

// GetBalance returns the native SOL balance of `pubkey` in lamports.
func (c *Client) GetBalance(ctx context.Context, pubkey string) (uint64, error) {
	if _, err := base58.Decode(pubkey); err != nil {
		return 0, fmt.Errorf("invalid public key: %w", err)
	}

	params := []any{pubkey, map[string]string{"commitment": "confirmed"}}

	var rpcResp balanceResponse
	if err := c.call(ctx, "getBalance", params, &rpcResp); err != nil {
		return 0, err
	}
	if rpcResp.Error != nil {
		return 0, fmt.Errorf("solana rpc error (%d): %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result.Value, nil
}

// GetBalanceSOL is a convenience wrapper that returns the balance in SOL.
func (c *Client) GetBalanceSOL(ctx context.Context, pubkey string) (float64, error) {
	lamports, err := c.GetBalance(ctx, pubkey)
	if err != nil {
		return 0, err
	}
	return float64(lamports) / float64(lamportsPerSOL), nil
}

// ---------------------------------------------------------------------------
// GetTokenBalance – returns the SPL-token balance for an owner + mint
// ---------------------------------------------------------------------------

// GetTokenBalance returns the raw token amount (as a string) and the
// UI-friendly float amount for the given owner and mint.
func (c *Client) GetTokenBalance(ctx context.Context, owner string, mint string) (rawAmount string, uiAmount float64, err error) {
	if _, err = base58.Decode(owner); err != nil {
		return "", 0, fmt.Errorf("invalid owner public key: %w", err)
	}
	if _, err = base58.Decode(mint); err != nil {
		return "", 0, fmt.Errorf("invalid mint address: %w", err)
	}

	params := []any{
		owner,
		map[string]string{"mint": mint},
		map[string]any{"encoding": "jsonParsed"},
	}

	var rpcResp tokenAccountsDetailedResponse
	if err = c.call(ctx, "getTokenAccountsByOwner", params, &rpcResp); err != nil {
		return "", 0, err
	}
	if rpcResp.Error != nil {
		return "", 0, fmt.Errorf("solana rpc error (%d): %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	for _, entry := range rpcResp.Result.Value {
		ta := entry.Account.Data.Parsed.Info.TokenAmount
		if ta.Amount != "" {
			return ta.Amount, ta.UIAmount, nil
		}
	}
	return "0", 0, nil
}

// ---------------------------------------------------------------------------
// RequestAirdrop – devnet/testnet only, request SOL from the faucet
// ---------------------------------------------------------------------------

type airdropResponse struct {
	Result string    `json:"result"` // transaction signature
	Error  *rpcError `json:"error"`
}

// RequestAirdrop requests `lamports` worth of SOL from the cluster faucet.
// This only works on devnet and testnet.  Returns the airdrop tx signature.
func (c *Client) RequestAirdrop(ctx context.Context, pubkey string, lamports uint64) (string, error) {
	if _, err := base58.Decode(pubkey); err != nil {
		return "", fmt.Errorf("invalid public key: %w", err)
	}

	params := []any{pubkey, lamports, map[string]string{"commitment": "confirmed"}}

	var rpcResp airdropResponse
	if err := c.call(ctx, "requestAirdrop", params, &rpcResp); err != nil {
		return "", err
	}
	if rpcResp.Error != nil {
		return "", fmt.Errorf("solana rpc error (%d): %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

// ---------------------------------------------------------------------------
// SendSOL – build, sign & send a native SOL transfer
// ---------------------------------------------------------------------------

type recentBlockhashResponse struct {
	Result struct {
		Value struct {
			Blockhash string `json:"blockhash"`
		} `json:"value"`
	} `json:"result"`
	Error *rpcError `json:"error"`
}

type sendTxResponse struct {
	Result string    `json:"result"` // tx signature (base58)
	Error  *rpcError `json:"error"`
}

// SendSOL transfers `lamports` of native SOL from the account controlled
// by `senderPrivateKey` to `recipientPubkey`.
// Returns the transaction signature on success.
func (c *Client) SendSOL(
	ctx context.Context,
	senderPrivateKey ed25519.PrivateKey,
	recipientPubkey string,
	lamports uint64,
) (string, error) {
	if len(senderPrivateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("invalid sender private key length: %d", len(senderPrivateKey))
	}

	toPubBytes, err := base58.Decode(recipientPubkey)
	if err != nil {
		return "", fmt.Errorf("invalid recipient public key: %w", err)
	}
	if len(toPubBytes) != ed25519.PublicKeySize {
		return "", fmt.Errorf("recipient public key has invalid length %d", len(toPubBytes))
	}

	fromPub := senderPrivateKey.Public().(ed25519.PublicKey)

	// 1. Fetch recent blockhash
	blockhash, err := c.getRecentBlockhash(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get recent blockhash: %w", err)
	}

	// 2. Build the transaction message
	msg := buildTransferMessage(fromPub, toPubBytes, blockhash, lamports)

	// 3. Sign
	sig := ed25519.Sign(senderPrivateKey, msg)

	// 4. Serialise the wire-format transaction
	txBytes := serializeTransaction(sig, msg)

	// 5. Send
	encoded := base58.Encode(txBytes)
	params := []any{
		encoded,
		map[string]any{
			"encoding":            "base58",
			"skipPreflight":       false,
			"preflightCommitment": "confirmed",
		},
	}

	var rpcResp sendTxResponse
	if err := c.call(ctx, "sendTransaction", params, &rpcResp); err != nil {
		return "", err
	}
	if rpcResp.Error != nil {
		return "", fmt.Errorf("solana rpc error (%d): %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func (c *Client) getRecentBlockhash(ctx context.Context) ([]byte, error) {
	params := []any{map[string]string{"commitment": "confirmed"}}

	var rpcResp recentBlockhashResponse
	if err := c.call(ctx, "getLatestBlockhash", params, &rpcResp); err != nil {
		return nil, err
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("solana rpc error (%d): %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	bhBytes, err := base58.Decode(rpcResp.Result.Value.Blockhash)
	if err != nil {
		return nil, fmt.Errorf("failed to decode blockhash: %w", err)
	}
	return bhBytes, nil
}

// ---------------------------------------------------------------------------
// Minimal Solana transaction builder (native transfer only)
// ---------------------------------------------------------------------------

// System program ID (all zeros, 32 bytes)
var systemProgramID [32]byte

// buildTransferMessage constructs the Solana "message" bytes for a simple
// SOL transfer (SystemProgram::Transfer instruction index = 2).
func buildTransferMessage(from ed25519.PublicKey, to []byte, recentBlockhash []byte, lamports uint64) []byte {
	var buf bytes.Buffer

	// --- Header ---
	// num_required_signatures
	buf.WriteByte(1)
	// num_readonly_signed_accounts
	buf.WriteByte(0)
	// num_readonly_unsigned_accounts
	buf.WriteByte(1) // system program

	// --- Account keys (3) ---
	// 0: from (signer, writable)
	buf.Write(from[:32])
	// 1: to (writable)
	buf.Write(to[:32])
	// 2: system program (readonly)
	buf.Write(systemProgramID[:])

	// --- Recent blockhash ---
	buf.Write(recentBlockhash[:32])

	// --- Instructions ---
	// compact-u16 count = 1
	buf.WriteByte(1)

	// Instruction:
	// program_id_index = 2 (system program)
	buf.WriteByte(2)
	// compact-u16 num_accounts = 2
	buf.WriteByte(2)
	// account indices
	buf.WriteByte(0) // from
	buf.WriteByte(1) // to

	// data: instruction index (u32 LE = 2 for Transfer) + lamports (u64 LE)
	// compact-u16 data_len = 12
	buf.WriteByte(12)
	// SystemInstruction::Transfer = 2u32
	instrIndex := make([]byte, 4)
	binary.LittleEndian.PutUint32(instrIndex, 2)
	buf.Write(instrIndex)
	// lamports u64 LE
	lamportBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(lamportBytes, lamports)
	buf.Write(lamportBytes)

	return buf.Bytes()
}

// serializeTransaction wraps a signed message into the Solana wire format.
func serializeTransaction(signature []byte, message []byte) []byte {
	var buf bytes.Buffer
	// compact-u16 num_signatures = 1
	buf.WriteByte(1)
	// signature (64 bytes)
	buf.Write(signature[:64])
	// message
	buf.Write(message)
	return buf.Bytes()
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type tokenAccountsResponse struct {
	Result struct {
		Value []struct {
			Account struct {
				Data struct {
					Parsed struct {
						Info struct {
							TokenAmount struct {
								Amount string `json:"amount"`
							} `json:"tokenAmount"`
						} `json:"info"`
					} `json:"parsed"`
				} `json:"data"`
			} `json:"account"`
		} `json:"value"`
	} `json:"result"`
	Error *rpcError `json:"error"`
}

type tokenAccountsDetailedResponse struct {
	Result struct {
		Value []struct {
			Account struct {
				Data struct {
					Parsed struct {
						Info struct {
							TokenAmount struct {
								Amount   string  `json:"amount"`
								Decimals int     `json:"decimals"`
								UIAmount float64 `json:"uiAmount"`
							} `json:"tokenAmount"`
						} `json:"info"`
					} `json:"parsed"`
				} `json:"data"`
			} `json:"account"`
		} `json:"value"`
	} `json:"result"`
	Error *rpcError `json:"error"`
}
