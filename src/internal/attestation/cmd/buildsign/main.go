// buildsign is a helper used during the release process.
//
// Usage:
//
//	# Generate a new Ed25519 keypair (one-time setup):
//	go run ./internal/attestation/cmd/buildsign keygen
//
//	# Sign a build (used in CI):
//	go run ./internal/attestation/cmd/buildsign sign <version> <commit> <private-key-hex>
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"opentela/internal/attestation"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: buildsign <keygen|sign> ...")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "keygen":
		keygen()
	case "sign":
		sign()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func keygen() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "keygen error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("public:  %s\n", hex.EncodeToString(pub))
	fmt.Printf("private: %s\n", hex.EncodeToString(priv))
	fmt.Println("\nPut the public key in attestation.go (maintainerPubKeyHex).")
	fmt.Println("Store the private key as a GitHub Actions secret (BUILD_SIGN_KEY).")
}

func sign() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: buildsign sign <version> <commit> <private-key-hex>")
		os.Exit(1)
	}
	version := os.Args[2]
	commit := os.Args[3]
	privHex := os.Args[4]

	privBytes, err := hex.DecodeString(privHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid private key hex: %v\n", err)
		os.Exit(1)
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		fmt.Fprintf(os.Stderr, "private key has wrong length: got %d, want %d\n", len(privBytes), ed25519.PrivateKeySize)
		os.Exit(1)
	}
	priv := ed25519.PrivateKey(privBytes)
	sig := attestation.Sign(priv, version, commit)
	fmt.Print(sig)
}
