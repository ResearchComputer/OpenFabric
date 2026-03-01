package wallet

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mr-tron/base58"
)

const (
	WalletTypeOCF    = "ocf"
	WalletTypeSolana = "solana"

	defaultAccountsFile = "accounts.json"
	legacyWalletFile    = "wallet.json"
	accountsDirName     = "accounts"

	// configDirName is the subdirectory under $HOME/.config used for all
	// OpenTela wallet data.  Changing this constant migrates the canonical
	// storage location.
	configDirName = "opentela"

	// legacyBaseDir is the old location (~/.ocf) so we can migrate
	// transparently.
	legacyBaseDirName = ".ocf"
)

// Account represents a single managed wallet account.
type Account struct {
	Type      string    `json:"type"`
	PublicKey string    `json:"public_key"`
	Private   string    `json:"private_key"`
	FilePath  string    `json:"file_path"`
	CreatedAt time.Time `json:"created_at"`
	// ProviderID is a deterministic, unique identifier derived from the
	// wallet public key.  It is used to tag services registered in the
	// OpenTela network.
	ProviderID string `json:"provider_id,omitempty"`
}

// ExportedWallet is the JSON structure written when the user asks for an
// exportable wallet file that can be imported into Phantom, Solflare, etc.
type ExportedWallet struct {
	PublicKey string `json:"public_key"`
	SecretKey []int  `json:"secret_key"`
	Mnemonic  string `json:"mnemonic,omitempty"` // reserved for future BIP-39
}

// WalletManager manages all locally-stored wallet accounts.
type WalletManager struct {
	storageDir  string
	storagePath string
	accounts    []Account
}

// NewWalletManager creates (or opens) the wallet store under
// ~/.config/opentela.  If the legacy ~/.ocf directory exists it is
// migrated automatically.
func NewWalletManager() (*WalletManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("unable to determine home directory: %w", err)
	}

	baseDir := filepath.Join(homeDir, ".config", configDirName)
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to ensure wallet directory: %w", err)
	}

	manager := &WalletManager{
		storageDir:  baseDir,
		storagePath: filepath.Join(baseDir, defaultAccountsFile),
	}

	// Attempt transparent migration from the legacy ~/.ocf location.
	if err := manager.migrateLegacyDir(homeDir); err != nil {
		// Non-fatal – log but keep going.
		fmt.Fprintf(os.Stderr, "wallet: legacy migration warning: %v\n", err)
	}

	if err := manager.loadAccounts(); err != nil {
		return nil, err
	}

	return manager, nil
}

// NewWalletManagerWithDir creates a WalletManager rooted at the supplied
// directory.  Useful for tests.
func NewWalletManagerWithDir(baseDir string) (*WalletManager, error) {
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to ensure wallet directory: %w", err)
	}
	manager := &WalletManager{
		storageDir:  baseDir,
		storagePath: filepath.Join(baseDir, defaultAccountsFile),
	}
	if err := manager.loadAccounts(); err != nil {
		return nil, err
	}
	return manager, nil
}

// ---------------------------------------------------------------------------
// Persistence
// ---------------------------------------------------------------------------

func (wm *WalletManager) loadAccounts() error {
	data, err := os.ReadFile(wm.storagePath)
	if errors.Is(err, os.ErrNotExist) {
		wm.accounts = []Account{}
		return wm.migrateLegacyWallet()
	}
	if err != nil {
		return fmt.Errorf("failed to read accounts file: %w", err)
	}

	var payload struct {
		Accounts []Account `json:"accounts"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("failed to parse accounts file: %w", err)
	}

	wm.accounts = payload.Accounts

	// Back-fill ProviderID for accounts that were persisted before this
	// field existed.
	dirty := false
	for i := range wm.accounts {
		if wm.accounts[i].ProviderID == "" && wm.accounts[i].PublicKey != "" {
			wm.accounts[i].ProviderID = deriveProviderID(wm.accounts[i].PublicKey)
			dirty = true
		}
	}
	if dirty {
		_ = wm.saveAccounts()
	}

	return nil
}

func (wm *WalletManager) migrateLegacyWallet() error {
	legacyPath := filepath.Join(wm.storageDir, legacyWalletFile)
	if _, err := os.Stat(legacyPath); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return fmt.Errorf("failed to migrate legacy wallet: %w", err)
	}

	privateBytes, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return fmt.Errorf("failed to decode legacy wallet: %w", err)
	}
	if len(privateBytes) != ed25519.PrivateKeySize {
		return fmt.Errorf("legacy wallet has invalid size")
	}

	pub := ed25519.PrivateKey(privateBytes).Public().(ed25519.PublicKey)
	pubEncoded := base64.StdEncoding.EncodeToString(pub)
	account := Account{
		Type:       WalletTypeOCF,
		PublicKey:  pubEncoded,
		Private:    base64.StdEncoding.EncodeToString(privateBytes),
		FilePath:   legacyPath,
		CreatedAt:  time.Now().UTC(),
		ProviderID: deriveProviderID(pubEncoded),
	}
	wm.accounts = append(wm.accounts, account)
	return wm.saveAccounts()
}

// migrateLegacyDir copies accounts.json and the accounts/ sub-tree from
// the old ~/.ocf directory into the new ~/.config/opentela directory, but
// only if the new directory does not already contain an accounts file.
func (wm *WalletManager) migrateLegacyDir(homeDir string) error {
	legacyDir := filepath.Join(homeDir, legacyBaseDirName)
	legacyAccountsFile := filepath.Join(legacyDir, defaultAccountsFile)

	// Nothing to migrate.
	if _, err := os.Stat(legacyAccountsFile); errors.Is(err, os.ErrNotExist) {
		return nil
	}

	// Already migrated (new location has accounts).
	if _, err := os.Stat(wm.storagePath); err == nil {
		return nil
	}

	// Copy accounts.json
	data, err := os.ReadFile(legacyAccountsFile)
	if err != nil {
		return fmt.Errorf("read legacy accounts: %w", err)
	}
	if err := os.WriteFile(wm.storagePath, data, 0o600); err != nil {
		return fmt.Errorf("write migrated accounts: %w", err)
	}

	// Copy keypair files inside accounts/
	legacyAccountsDir := filepath.Join(legacyDir, accountsDirName)
	newAccountsDir := filepath.Join(wm.storageDir, accountsDirName)
	if info, err := os.Stat(legacyAccountsDir); err == nil && info.IsDir() {
		if err := copyDirRecursive(legacyAccountsDir, newAccountsDir); err != nil {
			return fmt.Errorf("copy legacy keypairs: %w", err)
		}
	}

	// Also copy legacy wallet.json if present
	legacyWalletPath := filepath.Join(legacyDir, legacyWalletFile)
	if _, err := os.Stat(legacyWalletPath); err == nil {
		wData, err := os.ReadFile(legacyWalletPath)
		if err == nil {
			_ = os.WriteFile(filepath.Join(wm.storageDir, legacyWalletFile), wData, 0o600)
		}
	}

	// Update file paths inside the migrated accounts.json so they point
	// to the new location.
	var payload struct {
		Accounts []Account `json:"accounts"`
	}
	if err := json.Unmarshal(data, &payload); err == nil {
		changed := false
		for i := range payload.Accounts {
			old := payload.Accounts[i].FilePath
			if strings.Contains(old, legacyBaseDirName) {
				rel, err := filepath.Rel(legacyDir, old)
				if err == nil {
					payload.Accounts[i].FilePath = filepath.Join(wm.storageDir, rel)
					changed = true
				}
			}
		}
		if changed {
			if updated, err := json.MarshalIndent(payload, "", "  "); err == nil {
				_ = os.WriteFile(wm.storagePath, updated, 0o600)
			}
		}
	}

	return nil
}

func (wm *WalletManager) saveAccounts() error {
	payload := struct {
		Accounts []Account `json:"accounts"`
	}{
		Accounts: wm.accounts,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal accounts: %w", err)
	}
	if err := os.WriteFile(wm.storagePath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write accounts file: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Account management
// ---------------------------------------------------------------------------

// Accounts returns a defensive copy of all managed accounts.
func (wm *WalletManager) Accounts() []Account {
	out := make([]Account, len(wm.accounts))
	copy(out, wm.accounts)
	return out
}

// DefaultAccount returns the first account (the "active" wallet).
func (wm *WalletManager) DefaultAccount() (Account, error) {
	if len(wm.accounts) == 0 {
		return Account{}, errors.New("no managed accounts")
	}
	return wm.accounts[0], nil
}

// AddSolanaAccount generates a new Ed25519 keypair, persists it in
// Solana-CLI compatible format, and adds it to the managed account list.
func (wm *WalletManager) AddSolanaAccount() (Account, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Account{}, fmt.Errorf("failed to generate Solana keypair: %w", err)
	}

	pub58 := base58.Encode(public)
	accountDir := filepath.Join(wm.storageDir, accountsDirName, pub58)
	if err := os.MkdirAll(accountDir, 0o700); err != nil {
		return Account{}, fmt.Errorf("failed to create account directory: %w", err)
	}

	keypairPath := filepath.Join(accountDir, "keypair.json")
	if err := writeSolanaKeypair(keypairPath, private); err != nil {
		return Account{}, err
	}

	account := Account{
		Type:       WalletTypeSolana,
		PublicKey:  pub58,
		Private:    base64.StdEncoding.EncodeToString(private),
		FilePath:   keypairPath,
		CreatedAt:  time.Now().UTC(),
		ProviderID: deriveProviderID(pub58),
	}
	wm.accounts = append(wm.accounts, account)
	if err := wm.saveAccounts(); err != nil {
		return Account{}, err
	}
	return account, nil
}

// ImportSolanaKeypair imports an existing Solana-CLI-format keypair file
// (a JSON array of 64 byte-ints) and adds it as a managed account.
func (wm *WalletManager) ImportSolanaKeypair(path string) (Account, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Account{}, fmt.Errorf("failed to read keypair file: %w", err)
	}

	privKey, err := parseSolanaKeypairJSON(data)
	if err != nil {
		return Account{}, err
	}

	pub := privKey.Public().(ed25519.PublicKey)
	pub58 := base58.Encode(pub)

	// Check for duplicate
	for _, acc := range wm.accounts {
		if acc.PublicKey == pub58 {
			return acc, fmt.Errorf("account %s already exists", pub58)
		}
	}

	// Copy the keypair into our managed directory
	accountDir := filepath.Join(wm.storageDir, accountsDirName, pub58)
	if err := os.MkdirAll(accountDir, 0o700); err != nil {
		return Account{}, fmt.Errorf("failed to create account directory: %w", err)
	}
	managedPath := filepath.Join(accountDir, "keypair.json")
	if err := writeSolanaKeypair(managedPath, privKey); err != nil {
		return Account{}, err
	}

	account := Account{
		Type:       WalletTypeSolana,
		PublicKey:  pub58,
		Private:    base64.StdEncoding.EncodeToString(privKey),
		FilePath:   managedPath,
		CreatedAt:  time.Now().UTC(),
		ProviderID: deriveProviderID(pub58),
	}
	wm.accounts = append(wm.accounts, account)
	if err := wm.saveAccounts(); err != nil {
		return Account{}, err
	}
	return account, nil
}

// ExportKeypair writes the keypair of the given account in Solana-CLI
// compatible format (JSON int array of the 64-byte secret key) to the
// supplied path.  This file can be directly imported into Phantom,
// Solflare, or the Solana CLI.
func (wm *WalletManager) ExportKeypair(pubKey string, destPath string) error {
	acc, found := wm.FindByPublicKey(pubKey)
	if !found {
		return fmt.Errorf("account %s not found", pubKey)
	}

	privBytes, err := base64.StdEncoding.DecodeString(acc.Private)
	if err != nil {
		return fmt.Errorf("failed to decode private key: %w", err)
	}

	if err := writeSolanaKeypair(destPath, ed25519.PrivateKey(privBytes)); err != nil {
		return fmt.Errorf("failed to write exported keypair: %w", err)
	}
	return nil
}

// ExportBase58PrivateKey returns the private key encoded in base58.
// This is the format accepted by Phantom's "Import Private Key" flow.
func (wm *WalletManager) ExportBase58PrivateKey(pubKey string) (string, error) {
	acc, found := wm.FindByPublicKey(pubKey)
	if !found {
		return "", fmt.Errorf("account %s not found", pubKey)
	}

	privBytes, err := base64.StdEncoding.DecodeString(acc.Private)
	if err != nil {
		return "", fmt.Errorf("failed to decode private key: %w", err)
	}

	return base58.Encode(privBytes), nil
}

// ---------------------------------------------------------------------------
// Lookups
// ---------------------------------------------------------------------------

// FindByFile returns the account whose keypair lives at the given path.
func (wm *WalletManager) FindByFile(path string) (Account, bool) {
	for _, acc := range wm.accounts {
		if acc.FilePath == path {
			return acc, true
		}
	}
	return Account{}, false
}

// FindByPublicKey returns the account with the given public key.
func (wm *WalletManager) FindByPublicKey(pubKey string) (Account, bool) {
	for _, acc := range wm.accounts {
		if acc.PublicKey == pubKey {
			return acc, true
		}
	}
	return Account{}, false
}

// FindByProviderID returns the account with the given provider ID.
func (wm *WalletManager) FindByProviderID(providerID string) (Account, bool) {
	for _, acc := range wm.accounts {
		if acc.ProviderID == providerID {
			return acc, true
		}
	}
	return Account{}, false
}

// WalletExists returns true if at least one account is managed.
func (wm *WalletManager) WalletExists() bool {
	return len(wm.accounts) > 0
}

// ---------------------------------------------------------------------------
// Convenience getters (operate on the default / first account)
// ---------------------------------------------------------------------------

func (wm *WalletManager) GetPublicKey() string {
	if acc, err := wm.DefaultAccount(); err == nil {
		return acc.PublicKey
	}
	return ""
}

func (wm *WalletManager) GetPrivateKey() string {
	if acc, err := wm.DefaultAccount(); err == nil {
		return acc.Private
	}
	return ""
}

func (wm *WalletManager) GetWalletPath() string {
	if acc, err := wm.DefaultAccount(); err == nil {
		return acc.FilePath
	}
	return ""
}

func (wm *WalletManager) GetWalletType() string {
	if acc, err := wm.DefaultAccount(); err == nil {
		return acc.Type
	}
	return ""
}

// GetProviderID returns the deterministic provider ID of the default
// account.  This is what gets attached to every service registration.
func (wm *WalletManager) GetProviderID() string {
	if acc, err := wm.DefaultAccount(); err == nil {
		return acc.ProviderID
	}
	return ""
}

// GetPrivateKeyBytes decodes and returns the raw Ed25519 private key of
// the default account.
func (wm *WalletManager) GetPrivateKeyBytes() (ed25519.PrivateKey, error) {
	acc, err := wm.DefaultAccount()
	if err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(acc.Private)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(b) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key length %d", len(b))
	}
	return ed25519.PrivateKey(b), nil
}

// ---------------------------------------------------------------------------
// InitializeWallet is the high-level entry point used by the server to
// load the wallet at startup.  It fails if no managed accounts exist.
// ---------------------------------------------------------------------------

func InitializeWallet() (*WalletManager, error) {
	wm, err := NewWalletManager()
	if err != nil {
		return nil, err
	}
	if !wm.WalletExists() {
		return nil, errors.New("no managed wallets found; run `otela wallet create` to generate a Solana account")
	}
	return wm, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeSolanaKeypair writes the private key in the Solana-CLI compatible
// JSON int-array format.  The file is chmod 0600.
func writeSolanaKeypair(path string, private ed25519.PrivateKey) error {
	keyInts := make([]int, len(private))
	for i, b := range private {
		keyInts[i] = int(b)
	}
	data, err := json.MarshalIndent(keyInts, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode Solana keypair: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write Solana keypair: %w", err)
	}
	return nil
}

// parseSolanaKeypairJSON reads the Solana-CLI JSON int-array format and
// returns an ed25519 private key.
func parseSolanaKeypairJSON(data []byte) (ed25519.PrivateKey, error) {
	var ints []int
	if err := json.Unmarshal(data, &ints); err != nil {
		return nil, fmt.Errorf("failed to parse keypair JSON: %w", err)
	}
	if len(ints) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("keypair has invalid size %d (expected %d)", len(ints), ed25519.PrivateKeySize)
	}
	key := make([]byte, len(ints))
	for i, v := range ints {
		if v < 0 || v > 255 {
			return nil, fmt.Errorf("byte value out of range at index %d: %d", i, v)
		}
		key[i] = byte(v)
	}
	return ed25519.PrivateKey(key), nil
}

// deriveProviderID produces a short, deterministic identifier from the
// wallet's public key.  The format is "otela-<first12 chars of base58 pubkey>"
// which is human-readable, unique per key, and stable across restarts.
func deriveProviderID(publicKey string) string {
	id := publicKey
	if len(id) > 12 {
		id = id[:12]
	}
	return "otela-" + id
}

// copyDirRecursive copies a directory tree.
func copyDirRecursive(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}
