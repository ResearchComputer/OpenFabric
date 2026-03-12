package solana

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		expected string
	}{
		{
			name:     "custom endpoint",
			endpoint: "https://custom.example.com",
			expected: "https://custom.example.com",
		},
		{
			name:     "empty endpoint uses default",
			endpoint: "",
			expected: "https://api.mainnet-beta.solana.com",
		},
		{
			name:     "default endpoint explicitly",
			endpoint: "https://api.mainnet-beta.solana.com",
			expected: "https://api.mainnet-beta.solana.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.endpoint)
			if client.endpoint != tt.expected {
				t.Errorf("NewClient() endpoint = %v, want %v", client.endpoint, tt.expected)
			}
			if client.httpClient == nil {
				t.Error("NewClient() httpClient should not be nil")
			}
			if client.httpClient.Timeout != 15*time.Second {
				t.Errorf("NewClient() timeout = %v, want %v", client.httpClient.Timeout, 15*time.Second)
			}
		})
	}
}

func TestClientHasSPLToken(t *testing.T) {
	// Test data
	validOwner := "11111111111111111111111111111112"
	validMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" // USDC mint

	tests := []struct {
		name           string
		owner          string
		mint           string
		serverResponse string
		serverStatus   int
		expectedResult bool
		expectError    bool
	}{
		{
			name:           "valid owner with token balance",
			owner:          validOwner,
			mint:           validMint,
			serverResponse: `{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
					"value": [
						{
							"account": {
								"data": {
									"parsed": {
										"info": {
											"tokenAmount": {
												"amount": "1000000"
											}
										}
									}
								}
							}
						}
					]
				}
			}`,
			serverStatus:   200,
			expectedResult: true,
			expectError:    false,
		},
		{
			name:           "valid owner with zero token balance",
			owner:          validOwner,
			mint:           validMint,
			serverResponse: `{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
					"value": [
						{
							"account": {
								"data": {
									"parsed": {
										"info": {
											"tokenAmount": {
												"amount": "0"
											}
										}
									}
								}
							}
						}
					]
				}
			}`,
			serverStatus:   200,
			expectedResult: false,
			expectError:    false,
		},
		{
			name:           "valid owner with no token accounts",
			owner:          validOwner,
			mint:           validMint,
			serverResponse: `{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
					"value": []
				}
			}`,
			serverStatus:   200,
			expectedResult: false,
			expectError:    false,
		},
		{
			name:           "token account with empty amount",
			owner:          validOwner,
			mint:           validMint,
			serverResponse: `{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
					"value": [
						{
							"account": {
								"data": {
									"parsed": {
										"info": {
											"tokenAmount": {
												"amount": ""
											}
										}
									}
								}
							}
						}
					]
				}
			}`,
			serverStatus:   200,
			expectedResult: false,
			expectError:    false,
		},
		{
			name:           "invalid owner public key",
			owner:          "invalid-key",
			mint:           validMint,
			serverResponse: `{"jsonrpc": "2.0", "id": 1, "result": {"value": []}}`,
			serverStatus:   200,
			expectedResult: false,
			expectError:    true,
		},
		{
			name:           "invalid mint address",
			owner:          validOwner,
			mint:           "invalid-mint",
			serverResponse: `{"jsonrpc": "2.0", "id": 1, "result": {"value": []}}`,
			serverStatus:   200,
			expectedResult: false,
			expectError:    true,
		},
		{
			name:           "RPC server error",
			owner:          validOwner,
			mint:           validMint,
			serverResponse: `{
				"jsonrpc": "2.0",
				"id": 1,
				"error": {
					"code": -32602,
					"message": "Invalid params"
				}
			}`,
			serverStatus:   200,
			expectedResult: false,
			expectError:    true,
		},
		{
			name:           "HTTP error status",
			owner:          validOwner,
			mint:           validMint,
			serverResponse: `{"error": "Internal server error"}`,
			serverStatus:   500,
			expectedResult: false,
			expectError:    true,
		},
		{
			name:           "invalid JSON response",
			owner:          validOwner,
			mint:           validMint,
			serverResponse: `invalid json response`,
			serverStatus:   200,
			expectedResult: false,
			expectError:    true,
		},
		{
			name:           "malformed amount value",
			owner:          validOwner,
			mint:           validMint,
			serverResponse: `{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
					"value": [
						{
							"account": {
								"data": {
									"parsed": {
										"info": {
											"tokenAmount": {
												"amount": "not-a-number"
											}
										}
									}
								}
							}
						}
					]
				}
			}`,
			serverStatus:   200,
			expectedResult: false,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.serverStatus)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.serverResponse))
			}))
			defer server.Close()

			client := NewClient(server.URL)

			ctx := context.Background()
			result, err := client.HasSPLToken(ctx, tt.owner, tt.mint)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
				return
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if !tt.expectError && result != tt.expectedResult {
				t.Errorf("HasSPLToken() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestClientHasSPLTokenRequestFormat(t *testing.T) {
	// Test that the request is properly formatted
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		// Verify content type
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", ct)
		}

		// Verify request body
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
			return
		}

		if payload["jsonrpc"] != "2.0" {
			t.Errorf("Expected jsonrpc 2.0, got %v", payload["jsonrpc"])
		}
		if payload["method"] != "getTokenAccountsByOwner" {
			t.Errorf("Expected method getTokenAccountsByOwner, got %v", payload["method"])
		}

		params, ok := payload["params"].([]any)
		if !ok || len(params) != 3 {
			t.Errorf("Expected params array with 3 elements")
			return
		}

		// Send success response
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc": "2.0", "id": 1, "result": {"value": []}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()

	_, err := client.HasSPLToken(ctx, "11111111111111111111111111111112", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestClientHasSPLTokenContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response to test context cancellation
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc": "2.0", "id": 1, "result": {"value": []}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.HasSPLToken(ctx, "11111111111111111111111111111112", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	if err == nil {
		t.Error("Expected error due to context cancellation")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("Expected context cancellation error, got: %v", err)
	}
}

func TestClientHasSPLTokenNetworkError(t *testing.T) {
	// Test with an invalid endpoint to simulate network error
	client := NewClient("http://localhost:99999") // Port that's unlikely to be in use

	ctx := context.Background()
	_, err := client.HasSPLToken(ctx, "11111111111111111111111111111112", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")

	if err == nil {
		t.Error("Expected network error")
	}
	if !strings.Contains(err.Error(), "failed to query Solana RPC") {
		t.Errorf("Expected Solana RPC error, got: %v", err)
	}
}

func TestTokenAccountsResponseStruct(t *testing.T) {
	// Test that the response struct can properly unmarshal valid JSON
	jsonData := `{
		"result": {
			"value": [
				{
					"account": {
						"data": {
							"parsed": {
								"info": {
									"tokenAmount": {
										"amount": "1000000"
									}
								}
							}
						}
					}
				}
			]
		},
		"error": null
	}`

	var resp tokenAccountsResponse
	err := json.Unmarshal([]byte(jsonData), &resp)
	if err != nil {
		t.Errorf("Failed to unmarshal tokenAccountsResponse: %v", err)
	}

	if len(resp.Result.Value) != 1 {
		t.Errorf("Expected 1 result value, got %d", len(resp.Result.Value))
	}

	amount := resp.Result.Value[0].Account.Data.Parsed.Info.TokenAmount.Amount
	if amount != "1000000" {
		t.Errorf("Expected amount 1000000, got %s", amount)
	}
}

func TestTokenAccountsResponseErrorStruct(t *testing.T) {
	// Test that the response struct can properly unmarshal error responses
	jsonData := `{
		"result": {
			"value": []
		},
		"error": {
			"code": -32602,
			"message": "Invalid params"
		}
	}`

	var resp tokenAccountsResponse
	err := json.Unmarshal([]byte(jsonData), &resp)
	if err != nil {
		t.Errorf("Failed to unmarshal error response: %v", err)
	}

	if resp.Error == nil {
		t.Error("Expected error to be non-nil")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("Expected error code -32602, got %d", resp.Error.Code)
	}
	if resp.Error.Message != "Invalid params" {
		t.Errorf("Expected error message 'Invalid params', got %s", resp.Error.Message)
	}
}

func TestGetBalance_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"value":5000000000}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()

	balance, err := client.GetBalance(ctx, "11111111111111111111111111111112")
	if err != nil {
		t.Fatalf("GetBalance returned unexpected error: %v", err)
	}
	if balance != 5000000000 {
		t.Errorf("GetBalance = %d, want 5000000000", balance)
	}
}

func TestGetBalance_InvalidPubkey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"value":0}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()

	_, err := client.GetBalance(ctx, "invalid-key")
	if err == nil {
		t.Fatal("Expected error for invalid pubkey, got nil")
	}
	if !strings.Contains(err.Error(), "invalid public key") {
		t.Errorf("Expected 'invalid public key' error, got: %v", err)
	}
}

func TestGetBalance_RPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid request"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()

	_, err := client.GetBalance(ctx, "11111111111111111111111111111112")
	if err == nil {
		t.Fatal("Expected RPC error, got nil")
	}
	if !strings.Contains(err.Error(), "solana rpc error") {
		t.Errorf("Expected solana rpc error, got: %v", err)
	}
}

func TestGetBalanceSOL_ConvertsProperly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"value":2000000000}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()

	sol, err := client.GetBalanceSOL(ctx, "11111111111111111111111111111112")
	if err != nil {
		t.Fatalf("GetBalanceSOL returned unexpected error: %v", err)
	}
	if sol != 2.0 {
		t.Errorf("GetBalanceSOL = %f, want 2.0", sol)
	}
}

func TestGetTokenBalance_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"result": {
				"value": [{
					"account": {
						"data": {
							"parsed": {
								"info": {
									"tokenAmount": {
										"amount": "5000000",
										"decimals": 6,
										"uiAmount": 5.0
									}
								}
							}
						}
					}
				}]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()

	rawAmount, uiAmount, err := client.GetTokenBalance(ctx, "11111111111111111111111111111112", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	if err != nil {
		t.Fatalf("GetTokenBalance returned unexpected error: %v", err)
	}
	if rawAmount != "5000000" {
		t.Errorf("GetTokenBalance rawAmount = %s, want 5000000", rawAmount)
	}
	if uiAmount != 5.0 {
		t.Errorf("GetTokenBalance uiAmount = %f, want 5.0", uiAmount)
	}
}

func TestGetTokenBalance_NoTokenAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"result": {
				"value": []
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()

	rawAmount, uiAmount, err := client.GetTokenBalance(ctx, "11111111111111111111111111111112", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	if err != nil {
		t.Fatalf("GetTokenBalance returned unexpected error: %v", err)
	}
	if rawAmount != "0" {
		t.Errorf("GetTokenBalance rawAmount = %s, want \"0\"", rawAmount)
	}
	if uiAmount != 0.0 {
		t.Errorf("GetTokenBalance uiAmount = %f, want 0.0", uiAmount)
	}
}

func TestRequestAirdrop_Success(t *testing.T) {
	expectedSig := "5VERv8NMhJruPvbLXbNx4HPnBiEynvyAYLmRfkaoYpBJAMwUZcW8Y8S6jN7x5gXKcV42rXFk6Y5Qq8Bh2f8RBYGS"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"` + expectedSig + `"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()

	sig, err := client.RequestAirdrop(ctx, "11111111111111111111111111111112", 1000000000)
	if err != nil {
		t.Fatalf("RequestAirdrop returned unexpected error: %v", err)
	}
	if sig != expectedSig {
		t.Errorf("RequestAirdrop = %s, want %s", sig, expectedSig)
	}
}

func TestRequestAirdrop_InvalidPubkey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":""}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()

	_, err := client.RequestAirdrop(ctx, "invalid-key", 1000000000)
	if err == nil {
		t.Fatal("Expected error for invalid pubkey, got nil")
	}
	if !strings.Contains(err.Error(), "invalid public key") {
		t.Errorf("Expected 'invalid public key' error, got: %v", err)
	}
}

func TestBuildTransferMessage_Length(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("Failed to generate ed25519 key: %v", err)
	}
	blockhash := make([]byte, 32)
	msg := buildTransferMessage(pub, pub, blockhash, 1000)

	if len(msg) == 0 {
		t.Fatal("buildTransferMessage returned empty message")
	}

	// Expected length: 3 (header) + 96 (3 x 32-byte keys) + 32 (blockhash)
	// + 1 (instruction count) + 1 (program_id_index) + 1 (num_accounts)
	// + 2 (account indices) + 1 (data_len) + 12 (instruction data) = 149
	expectedLen := 3 + 96 + 32 + 1 + 1 + 1 + 2 + 1 + 12
	if len(msg) != expectedLen {
		t.Errorf("buildTransferMessage length = %d, want %d", len(msg), expectedLen)
	}

	// Verify consistency: calling with same inputs produces same output
	msg2 := buildTransferMessage(pub, pub, blockhash, 1000)
	if len(msg) != len(msg2) {
		t.Errorf("buildTransferMessage not consistent: len %d vs %d", len(msg), len(msg2))
	}
	for i := range msg {
		if msg[i] != msg2[i] {
			t.Errorf("buildTransferMessage not consistent at byte %d: %d vs %d", i, msg[i], msg2[i])
			break
		}
	}
}

func TestSerializeTransaction_Format(t *testing.T) {
	sig := make([]byte, 64)
	for i := range sig {
		sig[i] = byte(i)
	}
	message := []byte("test message payload")

	tx := serializeTransaction(sig, message)

	// First byte should be 0x01 (1 signature)
	if tx[0] != 0x01 {
		t.Errorf("serializeTransaction first byte = 0x%02x, want 0x01", tx[0])
	}

	// Bytes 1..64 should be the signature
	for i := 0; i < 64; i++ {
		if tx[1+i] != sig[i] {
			t.Errorf("serializeTransaction sig byte %d = 0x%02x, want 0x%02x", i, tx[1+i], sig[i])
			break
		}
	}

	// Remaining bytes should be the message
	msgStart := 1 + 64
	remaining := tx[msgStart:]
	if string(remaining) != string(message) {
		t.Errorf("serializeTransaction message = %q, want %q", remaining, message)
	}

	// Total length = 1 + 64 + len(message)
	expectedLen := 1 + 64 + len(message)
	if len(tx) != expectedLen {
		t.Errorf("serializeTransaction length = %d, want %d", len(tx), expectedLen)
	}
}