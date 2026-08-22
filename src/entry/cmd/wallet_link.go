package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"

	"opentela/internal/account"
	"opentela/internal/wallet"
)

// wallet.link_timeout bounds the whole handshake; challenges are single-use
// and expire five minutes after issue, so generous-but-finite is right.
const walletLinkTimeout = 30 * time.Second

var walletLinkCmd = &cobra.Command{
	Use:   "link",
	Short: "Link this node's wallet to your OpenTela Cloud account",
	Long: `Link the default wallet (~/.config/opentela) to your account at
cloud.opentela.ai so every peer this node runs is owned by that account.

One OpenTela account operates a single wallet, and every peer it claims is
owned by that wallet. Linking signs a server-issued challenge with the
wallet's private key — the key never leaves this machine.

Sign-in (pick one):
  --token <jwt>     paste a Neon Auth JWT (short-lived; see the console)
  --email <addr>    sign in with your console email; the CLI prompts for the
                    password and fetches a fresh JWT itself

Example:
  otela wallet link --email me@example.com
  otela wallet link --token "$(cat token.txt)"

Config equivalents (cfg.yaml or env, prefix OF_):
  account.token, account.email, account.api_url, account.neon_auth_url
  e.g. OF_ACCOUNT_TOKEN=... otela wallet link

If the account already has a wallet linked, the server rejects with 409;
remove the old wallet from the console first (only possible once it no
longer proves an active instance), or link a different account.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runWalletLink(cmd); err != nil {
			fmt.Printf("Link failed: %v\n", err)
			os.Exit(1)
		}
	},
}

func runWalletLink(cmd *cobra.Command) error {
	// ── 1. The wallet to link ────────────────────────────────────────
	wm, err := wallet.NewWalletManager()
	if err != nil {
		return fmt.Errorf("initialize wallet manager: %w", err)
	}
	privKey, err := wm.GetPrivateKeyBytes()
	if err != nil {
		return fmt.Errorf("load default wallet key: %w", err)
	}
	walletPubkey := wm.GetPublicKey()
	if walletPubkey == "" {
		return fmt.Errorf("no default wallet; run `otela wallet create` first")
	}

	// ── 2. A Neon Auth JWT for the operator's account ────────────────
	token := viper.GetString("account.token")
	if token == "" {
		token, err = obtainJWT(cmd)
		if err != nil {
			return err
		}
	}

	// ── 3. Challenge → sign → link ──────────────────────────────────
	baseURL := viper.GetString("account.api_url")
	if baseURL == "" {
		baseURL = account.DefaultAPIBaseURL
	}

	ctx, cancel := context.WithTimeout(context.Background(), walletLinkTimeout)
	defer cancel()

	client := &account.Client{BaseURL: baseURL, Bearer: token}
	linked, err := client.LinkWallet(ctx, walletPubkey, privKey)
	if err != nil {
		return explainLinkError(err)
	}

	fmt.Println("✔ Wallet linked to your OpenTela Cloud account")
	fmt.Printf("  Wallet:       %s\n", linked.Wallet)
	fmt.Printf("  Primary:      %v\n", linked.Primary)
	if !linked.CreatedAt.IsZero() {
		fmt.Printf("  Linked at:    %s\n", linked.CreatedAt.Format(time.RFC3339))
	}
	fmt.Println("\nNext: claim this node's peers from the console:")
	fmt.Println("  https://cloud.opentela.ai/account")
	return nil
}

// obtainJWT signs in with email+password against the Neon Auth server and
// returns the freshly-issued JWT. Used when --token is not given.
func obtainJWT(cmd *cobra.Command) (string, error) {
	email := viper.GetString("account.email")
	if email == "" {
		return "", fmt.Errorf("no sign-in method: pass --token <jwt>, or --email <addr> " +
			"(also settable as OF_ACCOUNT_TOKEN / OF_ACCOUNT_EMAIL)")
	}

	password := viper.GetString("account.password")
	if password == "" {
		var err error
		password, err = promptPassword()
		if err != nil {
			return "", err
		}
	}

	neonAuthURL := viper.GetString("account.neon_auth_url")
	if neonAuthURL == "" {
		neonAuthURL = account.DefaultNeonAuthURL
	}

	ctx, cancel := context.WithTimeout(context.Background(), walletLinkTimeout)
	defer cancel()
	jwt, err := account.SignInEmail(ctx, nil, neonAuthURL, email, password)
	if err != nil {
		return "", explainSignInError(err)
	}
	return jwt, nil
}

// promptPassword reads a password without echoing it. Non-interactive
// shells (CI, pipes) must provide --password or OF_ACCOUNT_PASSWORD instead.
func promptPassword() (string, error) {
	if !term.IsTerminal(int(syscall.Stdin)) {
		return "", fmt.Errorf("no password provided and stdin is not a terminal; " +
			"pass --password (or OF_ACCOUNT_PASSWORD), or use --token")
	}
	fmt.Print("Console password: ")
	raw, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("empty password")
	}
	return string(raw), nil
}

// explainLinkError turns the control-plane reply into operator-friendly text.
func explainLinkError(err error) error {
	var apiErr *account.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Status {
	case 401:
		return fmt.Errorf("sign-in token rejected (%v); get a fresh JWT or sign "+
			"in with --email", apiErr)
	case 409:
		// The server distinguishes the cases in its body; echo it and add the
		// actionable one-wallet-per-account hint.
		return fmt.Errorf("%v; if your account already owns a different wallet, "+
			"remove it from the console first (only possible once it no longer "+
			"proves an active instance)", apiErr)
	case 422:
		return fmt.Errorf("the challenge signature was rejected (%v); this wallet's "+
			"keypair does not match its public key", apiErr)
	default:
		return apiErr
	}
}

// explainSignInError adds next-step guidance to Better Auth replies.
func explainSignInError(err error) error {
	var apiErr *account.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Status {
	case 401, 403:
		return fmt.Errorf("sign-in rejected (%v); check the email/password, verify the "+
			"account email in the console, or sign in there and use --token", apiErr)
	default:
		return apiErr
	}
}

func init() {
	walletLinkCmd.Flags().String("token", "", "Neon Auth JWT for your cloud account (OF_ACCOUNT_TOKEN)")
	walletLinkCmd.Flags().String("email", "", "console sign-in email; the CLI fetches a fresh JWT (OF_ACCOUNT_EMAIL)")
	walletLinkCmd.Flags().String("password", "", "console password (prompted when omitted on a terminal; OF_ACCOUNT_PASSWORD)")
	walletLinkCmd.Flags().String("api-url", account.DefaultAPIBaseURL, "control-plane base URL (OF_ACCOUNT_API_URL)")
	walletLinkCmd.Flags().String("neon-auth-url", account.DefaultNeonAuthURL, "Neon Auth sign-in URL (OF_ACCOUNT_NEON_AUTH_URL)")

	_ = viper.BindPFlag("account.token", walletLinkCmd.Flags().Lookup("token"))
	_ = viper.BindPFlag("account.email", walletLinkCmd.Flags().Lookup("email"))
	_ = viper.BindPFlag("account.password", walletLinkCmd.Flags().Lookup("password"))
	_ = viper.BindPFlag("account.api_url", walletLinkCmd.Flags().Lookup("api-url"))
	_ = viper.BindPFlag("account.neon_auth_url", walletLinkCmd.Flags().Lookup("neon-auth-url"))

	walletCmd.AddCommand(walletLinkCmd)
}
