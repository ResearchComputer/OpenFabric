package wallet

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewWalletManager(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		setup       func(baseDir string)
		expectError bool
	}{
		{
			name:        "new wallet manager in clean directory",
			setup:       func(baseDir string) {},
			expectError: false,
		},
		{
			name: "existing accounts file",
			setup: func(baseDir string) {
				accountsFile := filepath.Join(baseDir, "accounts.json")
				accounts := struct {
					Accounts []Account `json:"accounts"`
				}{
					Accounts: []Account{
						{
							Type:      WalletTypeOCF,
							PublicKey: "test-public-key",
							Private:   "test-private-key",
							FilePath:  "test-path",
							CreatedAt: time.Now().UTC(),
						},
					},
				}
				data, err := json.MarshalIndent(accounts, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(accountsFile, data, 0600); err != nil {
					t.Fatal(err)
				}
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := filepath.Join(tempDir, tt.name)
			if err := os.MkdirAll(baseDir, 0700); err != nil {
				t.Fatal(err)
			}
			tt.setup(baseDir)

			wm, err := NewWalletManagerWithDir(baseDir)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if wm.storageDir == "" {
				t.Error("storageDir should not be empty")
			}
			if wm.storagePath == "" {
				t.Error("storagePath should not be empty")
			}
			if wm.accounts == nil {
				t.Error("accounts should be initialized, not nil")
			}
		})
	}
}

func TestWalletManagerLoadAccounts(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("load non-existent accounts file", func(t *testing.T) {
		baseDir := filepath.Join(tempDir, "load-none")
		if err := os.MkdirAll(baseDir, 0700); err != nil {
			t.Fatal(err)
		}

		wm := &WalletManager{
			storageDir:  baseDir,
			storagePath: filepath.Join(baseDir, "accounts.json"),
		}

		err := wm.loadAccounts()
		if err != nil {
			t.Errorf("Unexpected error loading non-existent file: %v", err)
		}
		if len(wm.accounts) != 0 {
			t.Errorf("Expected empty accounts, got %d", len(wm.accounts))
		}
	})

	t.Run("load valid accounts file", func(t *testing.T) {
		baseDir := filepath.Join(tempDir, "load-valid")
		if err := os.MkdirAll(baseDir, 0700); err != nil {
			t.Fatal(err)
		}

		accountsFile := filepath.Join(baseDir, "accounts.json")
		accounts := struct {
			Accounts []Account `json:"accounts"`
		}{
			Accounts: []Account{
				{
					Type:      WalletTypeSolana,
					PublicKey: "test-solana-key",
					Private:   "test-private-key",
					FilePath:  "test-path",
					CreatedAt: time.Now().UTC(),
				},
			},
		}
		data, err := json.MarshalIndent(accounts, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(accountsFile, data, 0600); err != nil {
			t.Fatal(err)
		}

		wm := &WalletManager{
			storageDir:  baseDir,
			storagePath: accountsFile,
		}

		err = wm.loadAccounts()
		if err != nil {
			t.Errorf("Unexpected error loading valid file: %v", err)
		}
		if len(wm.accounts) != 1 {
			t.Errorf("Expected 1 account, got %d", len(wm.accounts))
		}
		if wm.accounts[0].Type != WalletTypeSolana {
			t.Errorf("Expected account type %s, got %s", WalletTypeSolana, wm.accounts[0].Type)
		}
	})

	t.Run("load invalid JSON file", func(t *testing.T) {
		baseDir := filepath.Join(tempDir, "load-invalid")
		if err := os.MkdirAll(baseDir, 0700); err != nil {
			t.Fatal(err)
		}

		accountsFile := filepath.Join(baseDir, "accounts.json")
		if err := os.WriteFile(accountsFile, []byte("invalid json"), 0600); err != nil {
			t.Fatal(err)
		}

		wm := &WalletManager{
			storageDir:  baseDir,
			storagePath: accountsFile,
		}

		err := wm.loadAccounts()
		if err == nil {
			t.Error("Expected error loading invalid JSON")
		}
	})

	t.Run("back-fills ProviderID on load", func(t *testing.T) {
		baseDir := filepath.Join(tempDir, "backfill-provider")
		if err := os.MkdirAll(baseDir, 0700); err != nil {
			t.Fatal(err)
		}

		accountsFile := filepath.Join(baseDir, "accounts.json")
		accounts := struct {
			Accounts []Account `json:"accounts"`
		}{
			Accounts: []Account{
				{
					Type:       WalletTypeSolana,
					PublicKey:  "AbCdEfGhIjKlMnOpQrSt",
					Private:    "dummy",
					FilePath:   "dummy-path",
					CreatedAt:  time.Now().UTC(),
					ProviderID: "", // intentionally empty
				},
			},
		}
		data, err := json.MarshalIndent(accounts, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(accountsFile, data, 0600); err != nil {
			t.Fatal(err)
		}

		wm := &WalletManager{
			storageDir:  baseDir,
			storagePath: accountsFile,
		}
		if err := wm.loadAccounts(); err != nil {
			t.Fatal(err)
		}

		if wm.accounts[0].ProviderID == "" {
			t.Error("ProviderID should have been back-filled")
		}
		expected := deriveProviderID("AbCdEfGhIjKlMnOpQrSt")
		if wm.accounts[0].ProviderID != expected {
			t.Errorf("Expected ProviderID %q, got %q", expected, wm.accounts[0].ProviderID)
		}
	})
}

func TestWalletManagerMigrateLegacyWallet(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("no legacy wallet to migrate", func(t *testing.T) {
		baseDir := filepath.Join(tempDir, "no-legacy")
		if err := os.MkdirAll(baseDir, 0700); err != nil {
			t.Fatal(err)
		}

		wm := &WalletManager{
			storageDir: baseDir,
		}

		err := wm.migrateLegacyWallet()
		if err != nil {
			t.Errorf("Unexpected error when no legacy wallet exists: %v", err)
		}
		if len(wm.accounts) != 0 {
			t.Errorf("Expected no accounts after migration attempt, got %d", len(wm.accounts))
		}
	})

	t.Run("migrate valid legacy wallet", func(t *testing.T) {
		baseDir := filepath.Join(tempDir, "valid-legacy")
		if err := os.MkdirAll(baseDir, 0700); err != nil {
			t.Fatal(err)
		}

		// Generate a valid Ed25519 keypair
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}

		legacyPath := filepath.Join(baseDir, "wallet.json")
		privateEncoded := base64.StdEncoding.EncodeToString(private)
		if err := os.WriteFile(legacyPath, []byte(privateEncoded), 0600); err != nil {
			t.Fatal(err)
		}

		wm := &WalletManager{
			storageDir:  baseDir,
			storagePath: filepath.Join(baseDir, "accounts.json"),
		}

		err = wm.migrateLegacyWallet()
		if err != nil {
			t.Errorf("Unexpected error during migration: %v", err)
		}
		if len(wm.accounts) != 1 {
			t.Errorf("Expected 1 account after migration, got %d", len(wm.accounts))
		}

		account := wm.accounts[0]
		if account.Type != WalletTypeOCF {
			t.Errorf("Expected account type %s, got %s", WalletTypeOCF, account.Type)
		}
		if account.PublicKey != base64.StdEncoding.EncodeToString(public) {
			t.Error("Public key mismatch after migration")
		}
		if account.Private != privateEncoded {
			t.Error("Private key mismatch after migration")
		}
		if account.FilePath != legacyPath {
			t.Error("File path mismatch after migration")
		}
		if account.ProviderID == "" {
			t.Error("ProviderID should be set after migration")
		}
	})

	t.Run("migrate invalid legacy wallet - wrong size", func(t *testing.T) {
		baseDir := filepath.Join(tempDir, "invalid-legacy")
		if err := os.MkdirAll(baseDir, 0700); err != nil {
			t.Fatal(err)
		}

		legacyPath := filepath.Join(baseDir, "wallet.json")
		invalidKey := base64.StdEncoding.EncodeToString([]byte("too-short"))
		if err := os.WriteFile(legacyPath, []byte(invalidKey), 0600); err != nil {
			t.Fatal(err)
		}

		wm := &WalletManager{
			storageDir:  baseDir,
			storagePath: filepath.Join(baseDir, "accounts.json"),
		}

		err := wm.migrateLegacyWallet()
		if err == nil {
			t.Error("Expected error migrating invalid legacy wallet")
		}
	})
}

func TestWalletManagerSaveAccounts(t *testing.T) {
	tempDir := t.TempDir()

	wm := &WalletManager{
		storageDir:  tempDir,
		storagePath: filepath.Join(tempDir, "accounts.json"),
		accounts: []Account{
			{
				Type:       WalletTypeOCF,
				PublicKey:  "test-public-key",
				Private:    "test-private-key",
				FilePath:   "test-path",
				CreatedAt:  time.Now().UTC(),
				ProviderID: "otela-test-publi",
			},
		},
	}

	err := wm.saveAccounts()
	if err != nil {
		t.Errorf("Unexpected error saving accounts: %v", err)
	}

	data, err := os.ReadFile(wm.storagePath)
	if err != nil {
		t.Errorf("Failed to read saved accounts file: %v", err)
	}

	var payload struct {
		Accounts []Account `json:"accounts"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Errorf("Failed to unmarshal saved accounts: %v", err)
	}

	if len(payload.Accounts) != 1 {
		t.Errorf("Expected 1 saved account, got %d", len(payload.Accounts))
	}
	if payload.Accounts[0].ProviderID != "otela-test-publi" {
		t.Errorf("Expected ProviderID to be persisted, got %q", payload.Accounts[0].ProviderID)
	}
}

func TestWalletManagerAccounts(t *testing.T) {
	wm := &WalletManager{
		accounts: []Account{
			{Type: WalletTypeOCF, PublicKey: "key1"},
			{Type: WalletTypeSolana, PublicKey: "key2"},
		},
	}

	accounts := wm.Accounts()
	if len(accounts) != 2 {
		t.Errorf("Expected 2 accounts, got %d", len(accounts))
	}

	// Verify that modifying the returned slice doesn't affect the original
	accounts[0] = Account{Type: "modified"}
	if wm.accounts[0].Type == "modified" {
		t.Error("Modifying returned slice should not affect original")
	}
}

func TestWalletManagerDefaultAccount(t *testing.T) {
	tests := []struct {
		name        string
		accounts    []Account
		expectError bool
	}{
		{
			name:        "no accounts",
			accounts:    []Account{},
			expectError: true,
		},
		{
			name: "single account",
			accounts: []Account{
				{Type: WalletTypeOCF, PublicKey: "key1"},
			},
			expectError: false,
		},
		{
			name: "multiple accounts",
			accounts: []Account{
				{Type: WalletTypeOCF, PublicKey: "key1"},
				{Type: WalletTypeSolana, PublicKey: "key2"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wm := &WalletManager{accounts: tt.accounts}

			account, err := wm.DefaultAccount()
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if account != tt.accounts[0] {
				t.Error("Default account should be the first one")
			}
		})
	}
}

func TestWalletManagerAddSolanaAccount(t *testing.T) {
	tempDir := t.TempDir()

	wm, err := NewWalletManagerWithDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	account, err := wm.AddSolanaAccount()
	if err != nil {
		t.Errorf("Unexpected error adding Solana account: %v", err)
	}

	if account.Type != WalletTypeSolana {
		t.Errorf("Expected account type %s, got %s", WalletTypeSolana, account.Type)
	}
	if account.PublicKey == "" {
		t.Error("Public key should not be empty")
	}
	if account.Private == "" {
		t.Error("Private key should not be empty")
	}
	if account.FilePath == "" {
		t.Error("File path should not be empty")
	}
	if account.CreatedAt.IsZero() {
		t.Error("Created at should not be zero")
	}
	if account.ProviderID == "" {
		t.Error("ProviderID should not be empty")
	}
	if !strings.HasPrefix(account.ProviderID, "otela-") {
		t.Errorf("ProviderID should start with 'otela-', got %q", account.ProviderID)
	}

	// Verify account was added to manager
	if len(wm.accounts) != 1 {
		t.Errorf("Expected 1 account, got %d", len(wm.accounts))
	}

	// Verify keypair file was created
	if _, err := os.Stat(account.FilePath); os.IsNotExist(err) {
		t.Error("Keypair file should exist")
	}

	// Verify the saved keypair can be loaded
	data, err := os.ReadFile(account.FilePath)
	if err != nil {
		t.Errorf("Failed to read keypair file: %v", err)
	}

	var keyInts []int
	if err := json.Unmarshal(data, &keyInts); err != nil {
		t.Errorf("Failed to unmarshal keypair: %v", err)
	}

	if len(keyInts) != ed25519.PrivateKeySize {
		t.Errorf("Expected key size %d, got %d", ed25519.PrivateKeySize, len(keyInts))
	}
}

func TestWriteSolanaKeypair(t *testing.T) {
	tempDir := t.TempDir()
	keypairPath := filepath.Join(tempDir, "keypair.json")

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	err = writeSolanaKeypair(keypairPath, private)
	if err != nil {
		t.Errorf("Unexpected error writing keypair: %v", err)
	}

	info, err := os.Stat(keypairPath)
	if err != nil {
		t.Errorf("Failed to stat keypair file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("Expected file permissions 0600, got %o", info.Mode().Perm())
	}

	data, err := os.ReadFile(keypairPath)
	if err != nil {
		t.Errorf("Failed to read keypair file: %v", err)
	}

	var keyInts []int
	if err := json.Unmarshal(data, &keyInts); err != nil {
		t.Errorf("Failed to unmarshal keypair: %v", err)
	}

	if len(keyInts) != ed25519.PrivateKeySize {
		t.Errorf("Expected key size %d, got %d", ed25519.PrivateKeySize, len(keyInts))
	}

	reconstructed := make([]byte, len(keyInts))
	for i, keyInt := range keyInts {
		reconstructed[i] = byte(keyInt)
	}

	for i := range private {
		if reconstructed[i] != private[i] {
			t.Errorf("Key mismatch at position %d", i)
		}
	}
}

func TestParseSolanaKeypairJSON(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	keyInts := make([]int, len(private))
	for i, b := range private {
		keyInts[i] = int(b)
	}
	data, err := json.Marshal(keyInts)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := parseSolanaKeypairJSON(data)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(parsed) != ed25519.PrivateKeySize {
		t.Fatalf("Wrong size: got %d, want %d", len(parsed), ed25519.PrivateKeySize)
	}
	for i := range private {
		if parsed[i] != private[i] {
			t.Fatalf("Mismatch at index %d", i)
		}
	}

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := parseSolanaKeypairJSON([]byte("not json"))
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})

	t.Run("wrong size", func(t *testing.T) {
		_, err := parseSolanaKeypairJSON([]byte("[1,2,3]"))
		if err == nil {
			t.Error("Expected error for wrong size")
		}
	})

	t.Run("byte out of range", func(t *testing.T) {
		ints := make([]int, ed25519.PrivateKeySize)
		ints[0] = 999
		d, _ := json.Marshal(ints)
		_, err := parseSolanaKeypairJSON(d)
		if err == nil {
			t.Error("Expected error for out-of-range byte")
		}
	})
}

func TestWalletManagerImportSolanaKeypair(t *testing.T) {
	tempDir := t.TempDir()

	// Generate a keypair and write it to a file
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	srcFile := filepath.Join(tempDir, "source-keypair.json")
	if err := writeSolanaKeypair(srcFile, private); err != nil {
		t.Fatal(err)
	}

	managerDir := filepath.Join(tempDir, "managed")
	wm, err := NewWalletManagerWithDir(managerDir)
	if err != nil {
		t.Fatal(err)
	}

	account, err := wm.ImportSolanaKeypair(srcFile)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if account.Type != WalletTypeSolana {
		t.Errorf("Expected type %s, got %s", WalletTypeSolana, account.Type)
	}
	if account.PublicKey == "" {
		t.Error("PublicKey should not be empty")
	}
	if account.ProviderID == "" {
		t.Error("ProviderID should not be empty")
	}
	if account.FilePath == "" {
		t.Error("FilePath should not be empty")
	}

	// Verify managed keypair file exists and is valid
	data, err := os.ReadFile(account.FilePath)
	if err != nil {
		t.Fatalf("Failed to read managed keypair: %v", err)
	}
	parsedKey, err := parseSolanaKeypairJSON(data)
	if err != nil {
		t.Fatalf("Failed to parse managed keypair: %v", err)
	}
	importedPub := parsedKey.Public().(ed25519.PublicKey)
	for i := range pub {
		if importedPub[i] != pub[i] {
			t.Fatalf("Public key mismatch at index %d", i)
		}
	}

	// Importing the same key again should fail (duplicate)
	_, err = wm.ImportSolanaKeypair(srcFile)
	if err == nil {
		t.Error("Expected error importing duplicate keypair")
	}

	t.Run("import non-existent file", func(t *testing.T) {
		_, err := wm.ImportSolanaKeypair("/nonexistent/path.json")
		if err == nil {
			t.Error("Expected error importing non-existent file")
		}
	})
}

func TestWalletManagerExportKeypair(t *testing.T) {
	tempDir := t.TempDir()

	managerDir := filepath.Join(tempDir, "managed")
	wm, err := NewWalletManagerWithDir(managerDir)
	if err != nil {
		t.Fatal(err)
	}

	account, err := wm.AddSolanaAccount()
	if err != nil {
		t.Fatal(err)
	}

	exportPath := filepath.Join(tempDir, "exported-keypair.json")
	err = wm.ExportKeypair(account.PublicKey, exportPath)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Verify the exported file exists and is valid
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("Failed to read exported file: %v", err)
	}

	parsedKey, err := parseSolanaKeypairJSON(data)
	if err != nil {
		t.Fatalf("Failed to parse exported keypair: %v", err)
	}

	if len(parsedKey) != ed25519.PrivateKeySize {
		t.Fatalf("Expected key size %d, got %d", ed25519.PrivateKeySize, len(parsedKey))
	}

	// Verify the original source keypair matches the export
	srcData, err := os.ReadFile(account.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	srcKey, _ := parseSolanaKeypairJSON(srcData)
	for i := range srcKey {
		if parsedKey[i] != srcKey[i] {
			t.Fatalf("Key mismatch at index %d", i)
		}
	}

	t.Run("export unknown pubkey", func(t *testing.T) {
		err := wm.ExportKeypair("nonexistent-key", filepath.Join(tempDir, "nope.json"))
		if err == nil {
			t.Error("Expected error exporting unknown key")
		}
	})
}

func TestWalletManagerExportBase58PrivateKey(t *testing.T) {
	tempDir := t.TempDir()

	wm, err := NewWalletManagerWithDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	account, err := wm.AddSolanaAccount()
	if err != nil {
		t.Fatal(err)
	}

	b58, err := wm.ExportBase58PrivateKey(account.PublicKey)
	if err != nil {
		t.Fatalf("ExportBase58PrivateKey failed: %v", err)
	}
	if b58 == "" {
		t.Error("Base58 private key should not be empty")
	}

	// The base58-encoded key should be different from base64
	if b58 == account.Private {
		t.Error("Base58 encoding should differ from the stored base64 encoding")
	}

	t.Run("unknown pubkey", func(t *testing.T) {
		_, err := wm.ExportBase58PrivateKey("unknown-key")
		if err == nil {
			t.Error("Expected error for unknown pubkey")
		}
	})
}

func TestWalletManagerFindByFile(t *testing.T) {
	wm := &WalletManager{
		accounts: []Account{
			{Type: WalletTypeOCF, PublicKey: "key1", FilePath: "/path/to/file1"},
			{Type: WalletTypeSolana, PublicKey: "key2", FilePath: "/path/to/file2"},
		},
	}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "existing file path",
			path:     "/path/to/file1",
			expected: true,
		},
		{
			name:     "non-existing file path",
			path:     "/path/to/nonexistent",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, found := wm.FindByFile(tt.path)
			if found != tt.expected {
				t.Errorf("FindByFile(%s) = %v, want %v", tt.path, found, tt.expected)
			}
		})
	}
}

func TestWalletManagerFindByPublicKey(t *testing.T) {
	wm := &WalletManager{
		accounts: []Account{
			{Type: WalletTypeOCF, PublicKey: "key1", FilePath: "/path/1"},
			{Type: WalletTypeSolana, PublicKey: "key2", FilePath: "/path/2"},
		},
	}

	t.Run("found", func(t *testing.T) {
		acc, found := wm.FindByPublicKey("key2")
		if !found {
			t.Fatal("Expected to find account by public key")
		}
		if acc.Type != WalletTypeSolana {
			t.Errorf("Expected type %s, got %s", WalletTypeSolana, acc.Type)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, found := wm.FindByPublicKey("missing")
		if found {
			t.Error("Expected not to find non-existent public key")
		}
	})
}

func TestWalletManagerFindByProviderID(t *testing.T) {
	wm := &WalletManager{
		accounts: []Account{
			{Type: WalletTypeSolana, PublicKey: "key1", ProviderID: "otela-key1key1ke"},
			{Type: WalletTypeSolana, PublicKey: "key2", ProviderID: "otela-key2key2ke"},
		},
	}

	t.Run("found", func(t *testing.T) {
		acc, found := wm.FindByProviderID("otela-key2key2ke")
		if !found {
			t.Fatal("Expected to find account by provider ID")
		}
		if acc.PublicKey != "key2" {
			t.Errorf("Expected PublicKey key2, got %s", acc.PublicKey)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, found := wm.FindByProviderID("otela-nope")
		if found {
			t.Error("Expected not to find non-existent provider ID")
		}
	})
}

func TestWalletManagerWalletExists(t *testing.T) {
	tests := []struct {
		name     string
		accounts []Account
		expected bool
	}{
		{
			name:     "no accounts",
			accounts: []Account{},
			expected: false,
		},
		{
			name:     "has accounts",
			accounts: []Account{{Type: WalletTypeOCF}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wm := &WalletManager{accounts: tt.accounts}
			exists := wm.WalletExists()
			if exists != tt.expected {
				t.Errorf("WalletExists() = %v, want %v", exists, tt.expected)
			}
		})
	}
}

func TestWalletManagerGetMethods(t *testing.T) {
	wm := &WalletManager{
		accounts: []Account{
			{
				Type:       WalletTypeOCF,
				PublicKey:  "test-public-key",
				Private:    "test-private-key",
				FilePath:   "/test/path",
				ProviderID: "otela-test-publi",
			},
		},
	}

	if wm.GetPublicKey() != "test-public-key" {
		t.Errorf("GetPublicKey() = %v, want %v", wm.GetPublicKey(), "test-public-key")
	}

	if wm.GetPrivateKey() != "test-private-key" {
		t.Errorf("GetPrivateKey() = %v, want %v", wm.GetPrivateKey(), "test-private-key")
	}

	if wm.GetWalletPath() != "/test/path" {
		t.Errorf("GetWalletPath() = %v, want %v", wm.GetWalletPath(), "/test/path")
	}

	if wm.GetWalletType() != WalletTypeOCF {
		t.Errorf("GetWalletType() = %v, want %v", wm.GetWalletType(), WalletTypeOCF)
	}

	if wm.GetProviderID() != "otela-test-publi" {
		t.Errorf("GetProviderID() = %v, want %v", wm.GetProviderID(), "otela-test-publi")
	}

	// Test with empty accounts
	emptyWm := &WalletManager{accounts: []Account{}}

	if emptyWm.GetPublicKey() != "" {
		t.Error("GetPublicKey() should return empty string when no accounts")
	}
	if emptyWm.GetPrivateKey() != "" {
		t.Error("GetPrivateKey() should return empty string when no accounts")
	}
	if emptyWm.GetWalletPath() != "" {
		t.Error("GetWalletPath() should return empty string when no accounts")
	}
	if emptyWm.GetWalletType() != "" {
		t.Error("GetWalletType() should return empty string when no accounts")
	}
	if emptyWm.GetProviderID() != "" {
		t.Error("GetProviderID() should return empty string when no accounts")
	}
}

func TestGetPrivateKeyBytes(t *testing.T) {
	tempDir := t.TempDir()

	wm, err := NewWalletManagerWithDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	account, err := wm.AddSolanaAccount()
	if err != nil {
		t.Fatal(err)
	}

	privKey, err := wm.GetPrivateKeyBytes()
	if err != nil {
		t.Fatalf("GetPrivateKeyBytes() error: %v", err)
	}

	if len(privKey) != ed25519.PrivateKeySize {
		t.Fatalf("Expected key length %d, got %d", ed25519.PrivateKeySize, len(privKey))
	}

	// The base64-decoded private key should match
	decoded, err := base64.StdEncoding.DecodeString(account.Private)
	if err != nil {
		t.Fatal(err)
	}
	for i := range decoded {
		if privKey[i] != decoded[i] {
			t.Fatalf("Key mismatch at index %d", i)
		}
	}

	t.Run("empty manager", func(t *testing.T) {
		emptyWm := &WalletManager{accounts: []Account{}}
		_, err := emptyWm.GetPrivateKeyBytes()
		if err == nil {
			t.Error("Expected error from empty manager")
		}
	})
}

func TestDeriveProviderID(t *testing.T) {
	tests := []struct {
		pubkey   string
		expected string
	}{
		{
			pubkey:   "AbCdEfGhIjKlMnOpQrStUvWxYz",
			expected: "otela-AbCdEfGhIjKl",
		},
		{
			pubkey:   "Short",
			expected: "otela-Short",
		},
		{
			pubkey:   "ExactlyTwelv",
			expected: "otela-ExactlyTwelv",
		},
		{
			pubkey:   "",
			expected: "otela-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.pubkey, func(t *testing.T) {
			got := deriveProviderID(tt.pubkey)
			if got != tt.expected {
				t.Errorf("deriveProviderID(%q) = %q, want %q", tt.pubkey, got, tt.expected)
			}
		})
	}
}

func TestInitializeWallet(t *testing.T) {
	t.Run("with existing accounts", func(t *testing.T) {
		tempDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		defer os.Setenv("HOME", originalHome)
		os.Setenv("HOME", tempDir)

		// Pre-populate the new-style config directory
		configDir := filepath.Join(tempDir, ".config", "opentela")
		if err := os.MkdirAll(configDir, 0700); err != nil {
			t.Fatal(err)
		}

		accountsFile := filepath.Join(configDir, "accounts.json")
		accounts := struct {
			Accounts []Account `json:"accounts"`
		}{
			Accounts: []Account{
				{
					Type:       WalletTypeSolana,
					PublicKey:  "test-key",
					Private:    "test-private",
					FilePath:   "test-path",
					CreatedAt:  time.Now().UTC(),
					ProviderID: "otela-test-key",
				},
			},
		}
		data, err := json.MarshalIndent(accounts, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(accountsFile, data, 0600); err != nil {
			t.Fatal(err)
		}

		wm, err := InitializeWallet()
		if err != nil {
			t.Errorf("Unexpected error initializing wallet: %v", err)
		}
		if !wm.WalletExists() {
			t.Error("Wallet should exist after initialization")
		}
	})

	t.Run("without existing accounts", func(t *testing.T) {
		tempDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		defer os.Setenv("HOME", originalHome)
		os.Setenv("HOME", tempDir)

		wm, err := InitializeWallet()
		if err == nil {
			t.Error("Expected error when no managed wallets found")
		}
		if wm != nil {
			t.Error("Wallet manager should be nil when initialization fails")
		}
	})
}

func TestNewWalletManagerWithDir(t *testing.T) {
	tempDir := t.TempDir()

	wm, err := NewWalletManagerWithDir(tempDir)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if wm.storageDir != tempDir {
		t.Errorf("storageDir = %q, want %q", wm.storageDir, tempDir)
	}
	expectedPath := filepath.Join(tempDir, "accounts.json")
	if wm.storagePath != expectedPath {
		t.Errorf("storagePath = %q, want %q", wm.storagePath, expectedPath)
	}
}

func TestNewWalletManagerUsesOpentelaDir(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tempDir)

	wm, err := NewWalletManager()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expectedDir := filepath.Join(tempDir, ".config", "opentela")
	if wm.storageDir != expectedDir {
		t.Errorf("storageDir = %q, want %q", wm.storageDir, expectedDir)
	}

	// Verify the directory was created
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Error("Expected .config/opentela directory to be created")
	}
}

func TestMigrateLegacyDir(t *testing.T) {
	tempDir := t.TempDir()

	// Set up a legacy ~/.ocf directory with an accounts file
	legacyDir := filepath.Join(tempDir, ".ocf")
	if err := os.MkdirAll(legacyDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Create a legacy accounts file with a file path pointing to the old location
	legacyAccountsDir := filepath.Join(legacyDir, "accounts", "testkey")
	if err := os.MkdirAll(legacyAccountsDir, 0700); err != nil {
		t.Fatal(err)
	}

	keypairPath := filepath.Join(legacyAccountsDir, "keypair.json")
	if err := os.WriteFile(keypairPath, []byte("[1,2,3]"), 0600); err != nil {
		t.Fatal(err)
	}

	legacyAccounts := struct {
		Accounts []Account `json:"accounts"`
	}{
		Accounts: []Account{
			{
				Type:      WalletTypeSolana,
				PublicKey: "testkey",
				Private:   "dummy",
				FilePath:  keypairPath,
				CreatedAt: time.Now().UTC(),
			},
		},
	}
	data, err := json.MarshalIndent(legacyAccounts, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "accounts.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	// Create the new directory without an accounts file
	newDir := filepath.Join(tempDir, ".config", "opentela")
	if err := os.MkdirAll(newDir, 0700); err != nil {
		t.Fatal(err)
	}

	wm := &WalletManager{
		storageDir:  newDir,
		storagePath: filepath.Join(newDir, "accounts.json"),
	}

	err = wm.migrateLegacyDir(tempDir)
	if err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// The accounts file should now exist in the new location
	if _, err := os.Stat(wm.storagePath); os.IsNotExist(err) {
		t.Error("Accounts file should exist in new location after migration")
	}

	// The keypair directory should have been copied
	newKeypairPath := filepath.Join(newDir, "accounts", "testkey", "keypair.json")
	if _, err := os.Stat(newKeypairPath); os.IsNotExist(err) {
		t.Error("Keypair file should be copied to new location")
	}

	// File paths in the migrated accounts should point to the new location
	migratedData, err := os.ReadFile(wm.storagePath)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Accounts []Account `json:"accounts"`
	}
	if err := json.Unmarshal(migratedData, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Accounts) != 1 {
		t.Fatalf("Expected 1 account, got %d", len(payload.Accounts))
	}
	if !strings.Contains(payload.Accounts[0].FilePath, ".config/opentela") {
		t.Errorf("Migrated file path should reference new location, got %q", payload.Accounts[0].FilePath)
	}
}

func TestMigrateLegacyDirSkipsIfNewExists(t *testing.T) {
	tempDir := t.TempDir()

	// Set up both legacy and new directories with accounts files
	legacyDir := filepath.Join(tempDir, ".ocf")
	if err := os.MkdirAll(legacyDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "accounts.json"), []byte(`{"accounts":[]}`), 0600); err != nil {
		t.Fatal(err)
	}

	newDir := filepath.Join(tempDir, ".config", "opentela")
	if err := os.MkdirAll(newDir, 0700); err != nil {
		t.Fatal(err)
	}
	newAccountsPath := filepath.Join(newDir, "accounts.json")
	existingContent := `{"accounts":[{"type":"solana","public_key":"existing","private_key":"x","file_path":"y","created_at":"2024-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(newAccountsPath, []byte(existingContent), 0600); err != nil {
		t.Fatal(err)
	}

	wm := &WalletManager{
		storageDir:  newDir,
		storagePath: newAccountsPath,
	}

	err := wm.migrateLegacyDir(tempDir)
	if err != nil {
		t.Fatalf("Migration should succeed (no-op): %v", err)
	}

	// The new accounts file should be unchanged
	data, err := os.ReadFile(newAccountsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existingContent {
		t.Error("Existing accounts file should not be overwritten")
	}
}

func TestCopyDirRecursive(t *testing.T) {
	tempDir := t.TempDir()

	srcDir := filepath.Join(tempDir, "src")
	subDir := filepath.Join(srcDir, "sub")
	if err := os.MkdirAll(subDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "b.txt"), []byte("world"), 0600); err != nil {
		t.Fatal(err)
	}

	dstDir := filepath.Join(tempDir, "dst")
	err := copyDirRecursive(srcDir, dstDir)
	if err != nil {
		t.Fatalf("copyDirRecursive failed: %v", err)
	}

	aData, err := os.ReadFile(filepath.Join(dstDir, "a.txt"))
	if err != nil {
		t.Fatalf("Failed to read copied a.txt: %v", err)
	}
	if string(aData) != "hello" {
		t.Errorf("a.txt content = %q, want %q", string(aData), "hello")
	}

	bData, err := os.ReadFile(filepath.Join(dstDir, "sub", "b.txt"))
	if err != nil {
		t.Fatalf("Failed to read copied sub/b.txt: %v", err)
	}
	if string(bData) != "world" {
		t.Errorf("sub/b.txt content = %q, want %q", string(bData), "world")
	}
}

func TestRoundTripCreateExportImport(t *testing.T) {
	// End-to-end: create a wallet, export it, import into a second manager
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	exportDir := t.TempDir()

	wm1, err := NewWalletManagerWithDir(dir1)
	if err != nil {
		t.Fatal(err)
	}
	acc, err := wm1.AddSolanaAccount()
	if err != nil {
		t.Fatal(err)
	}

	// Export
	exportedPath := filepath.Join(exportDir, "exported.json")
	if err := wm1.ExportKeypair(acc.PublicKey, exportedPath); err != nil {
		t.Fatal(err)
	}

	// Import into second manager
	wm2, err := NewWalletManagerWithDir(dir2)
	if err != nil {
		t.Fatal(err)
	}
	importedAcc, err := wm2.ImportSolanaKeypair(exportedPath)
	if err != nil {
		t.Fatal(err)
	}

	if importedAcc.PublicKey != acc.PublicKey {
		t.Errorf("Imported public key %q != original %q", importedAcc.PublicKey, acc.PublicKey)
	}
	if importedAcc.ProviderID != acc.ProviderID {
		t.Errorf("Imported provider ID %q != original %q", importedAcc.ProviderID, acc.ProviderID)
	}
}
