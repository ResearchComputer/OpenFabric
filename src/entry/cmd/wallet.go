package cmd

import (
	"context"
	"fmt"
	"opentela/internal/solana"
	"opentela/internal/wallet"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var walletCmd = &cobra.Command{
	Use:   "wallet",
	Short: "Wallet management commands",
	Long: `Manage Solana wallets used by OpenTela for service ownership.

Wallets are stored under ~/.config/opentela and are automatically loaded
when you start a node. Generated keypairs are compatible with Phantom,
Solflare, and the Solana CLI (solana-keygen).`,
}

// ── create ──────────────────────────────────────────────────────────────

var walletCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new Solana wallet managed by OpenTela",
	Long: `Generate a new Ed25519 keypair and store it under
~/.config/opentela/accounts/<pubkey>/keypair.json.

The keypair file uses the Solana-CLI JSON int-array format so it can be
imported directly into Phantom, Solflare, or any tool that accepts
solana-keygen output.`,
	Run: func(cmd *cobra.Command, args []string) {
		wm, err := wallet.NewWalletManager()
		if err != nil {
			fmt.Printf("Failed to initialize wallet manager: %v\n", err)
			return
		}

		account, err := wm.AddSolanaAccount()
		if err != nil {
			fmt.Printf("Failed to create Solana account: %v\n", err)
			return
		}

		fmt.Println("✔ Created Solana wallet")
		fmt.Printf("  Public key:   %s\n", account.PublicKey)
		fmt.Printf("  Provider ID:  %s\n", account.ProviderID)
		fmt.Printf("  Keypair file: %s\n", account.FilePath)

		if len(wm.Accounts()) == 1 {
			fmt.Println("\nThis wallet is set as the default account.")
		} else {
			fmt.Println("\nUse `otela wallet list` to view all managed wallets.")
		}

		fmt.Println("\nTo import into Phantom or Solflare:")
		fmt.Println("  otela wallet export --pubkey " + account.PublicKey)
	},
}

// ── list ────────────────────────────────────────────────────────────────

var walletListCmd = &cobra.Command{
	Use:   "list",
	Short: "List managed wallets",
	Run: func(cmd *cobra.Command, args []string) {
		wm, err := wallet.NewWalletManager()
		if err != nil {
			fmt.Printf("Failed to initialize wallet manager: %v\n", err)
			return
		}

		accounts := wm.Accounts()
		if len(accounts) == 0 {
			fmt.Println("No wallets managed by OpenTela. Run `otela wallet create` to generate one.")
			return
		}

		for idx, account := range accounts {
			prefix := " "
			if idx == 0 {
				prefix = "*" // default
			}
			fmt.Printf("%s [%d] %s  (%s)\n", prefix, idx, account.PublicKey, account.Type)
			fmt.Printf("      provider-id: %s\n", account.ProviderID)
			fmt.Printf("      stored at:   %s\n", account.FilePath)
			fmt.Printf("      created:     %s\n", account.CreatedAt.Format(time.RFC3339))
		}
	},
}

// ── info ────────────────────────────────────────────────────────────────

var walletInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show the default wallet information",
	Run: func(cmd *cobra.Command, args []string) {
		wm, err := wallet.NewWalletManager()
		if err != nil {
			fmt.Printf("Failed to initialize wallet manager: %v\n", err)
			return
		}

		account, err := wm.DefaultAccount()
		if err != nil {
			fmt.Println("No default wallet configured. Run `otela wallet create` to create one.")
			return
		}

		fmt.Printf("Public key:   %s\n", account.PublicKey)
		fmt.Printf("Provider ID:  %s\n", account.ProviderID)
		fmt.Printf("Wallet type:  %s\n", account.Type)
		fmt.Printf("Keypair file: %s\n", account.FilePath)
		fmt.Printf("Created at:   %s\n", account.CreatedAt.Format(time.RFC3339))
	},
}

// ── export ──────────────────────────────────────────────────────────────

var walletExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export a wallet keypair for use in third-party wallets",
	Long: `Export the wallet's private key so it can be imported into
Phantom, Solflare, MetaMask (via Solana Snap), or the Solana CLI.

By default the base58-encoded private key is printed to stdout (the
format Phantom's "Import Private Key" flow expects).

With --file <path> the keypair is written in Solana-CLI JSON int-array
format instead, which can be loaded with:
  solana-keygen recover <path>
or dragged into Solflare's import dialog.`,
	Run: func(cmd *cobra.Command, args []string) {
		wm, err := wallet.NewWalletManager()
		if err != nil {
			fmt.Printf("Failed to initialize wallet manager: %v\n", err)
			return
		}

		pubkey, _ := cmd.Flags().GetString("pubkey")
		if pubkey == "" {
			acc, err := wm.DefaultAccount()
			if err != nil {
				fmt.Println("No default wallet. Specify --pubkey or run `otela wallet create`.")
				return
			}
			pubkey = acc.PublicKey
		}

		filePath, _ := cmd.Flags().GetString("file")
		if filePath != "" {
			if err := wm.ExportKeypair(pubkey, filePath); err != nil {
				fmt.Printf("Export failed: %v\n", err)
				return
			}
			fmt.Printf("✔ Keypair written to %s (Solana-CLI format)\n", filePath)
			fmt.Println("  You can import this file into Solflare or use it with the Solana CLI.")
			return
		}

		// Default: print base58 private key (Phantom import format)
		b58, err := wm.ExportBase58PrivateKey(pubkey)
		if err != nil {
			fmt.Printf("Export failed: %v\n", err)
			return
		}

		fmt.Println("Base58 private key (paste into Phantom → Import Private Key):")
		fmt.Println()
		fmt.Println(b58)
		fmt.Println()
		fmt.Println("⚠  Keep this key secret — anyone with access can control this wallet.")
	},
}

// ── import ──────────────────────────────────────────────────────────────

var walletImportCmd = &cobra.Command{
	Use:   "import <keypair.json>",
	Short: "Import an existing Solana keypair file",
	Long: `Import a Solana-CLI compatible keypair file (JSON int-array of 64
byte values). The file is copied into the managed wallet store under
~/.config/opentela.

You can generate a keypair with:
  solana-keygen new --outfile keypair.json

or export one from Solflare / Phantom and convert it.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		wm, err := wallet.NewWalletManager()
		if err != nil {
			fmt.Printf("Failed to initialize wallet manager: %v\n", err)
			return
		}

		account, err := wm.ImportSolanaKeypair(args[0])
		if err != nil {
			fmt.Printf("Import failed: %v\n", err)
			return
		}

		fmt.Println("✔ Imported Solana wallet")
		fmt.Printf("  Public key:   %s\n", account.PublicKey)
		fmt.Printf("  Provider ID:  %s\n", account.ProviderID)
		fmt.Printf("  Keypair file: %s\n", account.FilePath)
	},
}

// ── balance ─────────────────────────────────────────────────────────────

var walletBalanceCmd = &cobra.Command{
	Use:   "balance",
	Short: "Show the SOL and token balance of the default wallet",
	Long: `Query the Solana cluster for the native SOL balance of the
default wallet and, if a mint is configured, the SPL token balance.

By default the mainnet-beta RPC is used. Override with --solana.rpc.`,
	Run: func(cmd *cobra.Command, args []string) {
		wm, err := wallet.NewWalletManager()
		if err != nil {
			fmt.Printf("Failed to initialize wallet manager: %v\n", err)
			return
		}

		acc, err := wm.DefaultAccount()
		if err != nil {
			fmt.Println("No default wallet. Run `otela wallet create` first.")
			return
		}

		rpcEndpoint := viper.GetString("solana.rpc")
		if rpcEndpoint == "" {
			rpcEndpoint = defaultConfig.Solana.RPC
		}
		client := solana.NewClient(rpcEndpoint)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		pubkey := acc.PublicKey
		fmt.Printf("Wallet: %s\n", pubkey)
		fmt.Printf("RPC:    %s\n\n", rpcEndpoint)

		// SOL balance
		solBal, err := client.GetBalanceSOL(ctx, pubkey)
		if err != nil {
			fmt.Printf("  SOL balance: (error: %v)\n", err)
		} else {
			fmt.Printf("  SOL balance: %.9f SOL\n", solBal)
		}

		// SPL token balance (if mint configured)
		mint := viper.GetString("solana.mint")
		if mint != "" {
			raw, ui, err := client.GetTokenBalance(ctx, pubkey, mint)
			if err != nil {
				fmt.Printf("  Token (%s): (error: %v)\n", mint, err)
			} else {
				fmt.Printf("  Token (%s): %s (%.6f)\n", mint, raw, ui)
			}
		}
	},
}

// ── transfer ────────────────────────────────────────────────────────────

var walletTransferCmd = &cobra.Command{
	Use:   "transfer <recipient> <amount_sol>",
	Short: "Transfer SOL to another wallet",
	Long: `Send native SOL from the default wallet to the specified recipient.

  otela wallet transfer <recipient-pubkey> <amount-in-SOL>

Example:
  otela wallet transfer 5YNmS1R9nNSCDzb5a7mMJ1dwK9uHeAAF4CerBTpfXoA4 0.5

The amount is specified in SOL (not lamports). Fractional values are
supported up to 9 decimal places.`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		recipient := args[0]
		amountStr := args[1]

		amountSOL, err := strconv.ParseFloat(amountStr, 64)
		if err != nil || amountSOL <= 0 {
			fmt.Println("Invalid amount. Specify a positive number of SOL (e.g. 0.5).")
			return
		}
		lamports := uint64(amountSOL * 1_000_000_000)

		wm, err := wallet.NewWalletManager()
		if err != nil {
			fmt.Printf("Failed to initialize wallet manager: %v\n", err)
			return
		}

		privKey, err := wm.GetPrivateKeyBytes()
		if err != nil {
			fmt.Printf("Failed to load signing key: %v\n", err)
			return
		}

		acc, _ := wm.DefaultAccount()
		fmt.Printf("From:   %s\n", acc.PublicKey)
		fmt.Printf("To:     %s\n", recipient)
		fmt.Printf("Amount: %.9f SOL (%d lamports)\n\n", amountSOL, lamports)

		rpcEndpoint := viper.GetString("solana.rpc")
		if rpcEndpoint == "" {
			rpcEndpoint = defaultConfig.Solana.RPC
		}
		client := solana.NewClient(rpcEndpoint)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		sig, err := client.SendSOL(ctx, privKey, recipient, lamports)
		if err != nil {
			fmt.Printf("Transfer failed: %v\n", err)
			return
		}

		fmt.Printf("✔ Transaction sent!\n")
		fmt.Printf("  Signature: %s\n", sig)
		fmt.Printf("  Explorer:  https://explorer.solana.com/tx/%s\n", sig)
	},
}

// ── airdrop (devnet helper) ─────────────────────────────────────────────

var walletAirdropCmd = &cobra.Command{
	Use:   "airdrop [amount_sol]",
	Short: "Request a devnet/testnet SOL airdrop",
	Long: `Request free SOL from the Solana faucet. This only works on
devnet and testnet clusters. The default amount is 1 SOL.

Make sure your RPC endpoint points to devnet:
  otela wallet airdrop --solana.rpc https://api.devnet.solana.com 2`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		wm, err := wallet.NewWalletManager()
		if err != nil {
			fmt.Printf("Failed to initialize wallet manager: %v\n", err)
			return
		}
		acc, err := wm.DefaultAccount()
		if err != nil {
			fmt.Println("No default wallet. Run `otela wallet create` first.")
			return
		}

		amountSOL := 1.0
		if len(args) == 1 {
			amountSOL, err = strconv.ParseFloat(args[0], 64)
			if err != nil || amountSOL <= 0 {
				fmt.Println("Invalid amount.")
				return
			}
		}
		lamports := uint64(amountSOL * 1_000_000_000)

		rpcEndpoint := viper.GetString("solana.rpc")
		if rpcEndpoint == "" {
			rpcEndpoint = "https://api.devnet.solana.com"
		}
		client := solana.NewClient(rpcEndpoint)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		fmt.Printf("Requesting %.1f SOL airdrop for %s ...\n", amountSOL, acc.PublicKey)
		sig, err := client.RequestAirdrop(ctx, acc.PublicKey, lamports)
		if err != nil {
			fmt.Printf("Airdrop failed: %v\n", err)
			return
		}
		fmt.Printf("✔ Airdrop requested (tx: %s)\n", sig)
	},
}

// ── wire everything up ─────────────────────────────────────────────────

func init() {
	walletExportCmd.Flags().String("pubkey", "", "public key of the wallet to export (default: the active wallet)")
	walletExportCmd.Flags().String("file", "", "write keypair in Solana-CLI JSON format to this path instead of printing base58")

	walletBalanceCmd.Flags().String("solana.rpc", "", "Solana RPC endpoint override")
	walletTransferCmd.Flags().String("solana.rpc", "", "Solana RPC endpoint override")
	walletAirdropCmd.Flags().String("solana.rpc", "", "Solana RPC endpoint override")

	walletCmd.AddCommand(walletCreateCmd)
	walletCmd.AddCommand(walletListCmd)
	walletCmd.AddCommand(walletInfoCmd)
	walletCmd.AddCommand(walletExportCmd)
	walletCmd.AddCommand(walletImportCmd)
	walletCmd.AddCommand(walletBalanceCmd)
	walletCmd.AddCommand(walletTransferCmd)
	walletCmd.AddCommand(walletAirdropCmd)
}
