package server

import (
	"net/http"
	"opentela/internal/common"
	"opentela/internal/protocol"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// Access control policies for incoming requests on worker nodes.
//
// Configuration (cfg.yaml):
//
//	security:
//	  access_control:
//	    policy: "any"          # "any" | "self" | "whitelist" | "blacklist"
//	    whitelist:             # used when policy = "whitelist"
//	      - "5WalletPubkey1..."
//	      - "7WalletPubkey2..."
//	    blacklist:             # used when policy = "blacklist"
//	      - "5BannedWallet..."
//
// Policies:
//   - "any"       — accept requests from any peer (default, backward-compatible)
//   - "self"      — only accept requests from peers whose wallet matches our own
//   - "whitelist" — only accept requests from peers whose wallet is in the list
//   - "blacklist" — accept from everyone except wallets in the list

// resolveCallerWallet determines who is making this request.
//
// Priority:
//  1. X-Otela-Client-Wallet header (set by the head node after verifying the
//     end-user's bearer token against the auth server).  This represents the
//     actual end-user, not the forwarding head node.
//  2. The libp2p peer ID of the direct caller (the head node), looked up in
//     the node table.  If the peer has a verified identity attestation
//     (TrustLevel >= TrustSelfAttested), its WalletPubkey is returned because
//     that is the cryptographically proven wallet key.  Otherwise Owner is
//     used as a fallback (backward-compatible with peers that have no
//     attestation).
//
// Returns the wallet public key, or "" if the caller cannot be identified.
func resolveCallerWallet(r *http.Request) string {
	if !isLibp2pRemoteAddr(r.RemoteAddr) {
		return ""
	}

	// Only trust X-Otela-Client-Wallet when the request arrived over libp2p,
	// meaning the direct caller is a verified peer (the head node). A plain
	// HTTP caller could forge this header to bypass access control.
	if clientWallet := r.Header.Get("X-Otela-Client-Wallet"); clientWallet != "" {
		return clientWallet
	}

	addr := r.RemoteAddr
	peer, err := protocol.GetPeerFromTable(addr)
	if err != nil {
		return ""
	}
	// Prefer the cryptographically verified wallet pubkey from the identity
	// attestation when available; fall back to Owner for backward compat.
	if peer.TrustLevel >= protocol.TrustSelfAttested && peer.IdentityAttestation != nil {
		return peer.IdentityAttestation.WalletPubkey
	}
	return peer.Owner
}

// accessControlMiddleware returns a Gin middleware that enforces the
// operator's access control policy on incoming forwarded requests.
func accessControlMiddleware() gin.HandlerFunc {
	policy := strings.ToLower(viper.GetString("security.access_control.policy"))
	if policy == "" {
		policy = "any"
	}

	// Fast path: no restrictions.
	if policy == "any" {
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) {
		callerWallet := resolveCallerWallet(c.Request)

		// Re-read policy each time so config changes take effect without
		// restart (viper supports hot-reload).
		currentPolicy := strings.ToLower(viper.GetString("security.access_control.policy"))
		if currentPolicy == "" || currentPolicy == "any" {
			c.Next()
			return
		}

		switch currentPolicy {
		case "self":
			myWallet := viper.GetString("wallet.account")
			if myWallet == "" {
				common.Logger.Warn("access_control: policy=self but no wallet configured; denying request")
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "access denied: node has policy=self but no wallet configured",
				})
				return
			}
			if callerWallet != myWallet {
				common.Logger.Warnf("access_control: denied wallet=%s (policy=self)", callerWallet)
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "access denied: only requests from the node operator's own wallet are accepted",
				})
				return
			}

		case "whitelist":
			allowed := viper.GetStringSlice("security.access_control.whitelist")
			if !containsWallet(allowed, callerWallet) {
				common.Logger.Warnf("access_control: denied wallet=%s (not in whitelist)", callerWallet)
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "access denied: wallet not in whitelist",
				})
				return
			}

		case "blacklist":
			blocked := viper.GetStringSlice("security.access_control.blacklist")
			if containsWallet(blocked, callerWallet) {
				common.Logger.Warnf("access_control: denied wallet=%s (blacklisted)", callerWallet)
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "access denied: wallet is blacklisted",
				})
				return
			}

		default:
			common.Logger.Warnf("access_control: unknown policy %q, allowing request", currentPolicy)
		}

		c.Next()
	}
}

// isLibp2pRemoteAddr returns true when addr looks like a libp2p peer ID.
// libp2p-http sets http.Request.RemoteAddr to the raw peer ID string
// (no host:port). Known base58-encoded prefixes:
//   - "12D3" — Ed25519/identity multihash (current default)
//   - "Qm"   — SHA2-256 multihash (legacy RSA keys)
//   - "16Ui" — secp256k1 keys
func isLibp2pRemoteAddr(addr string) bool {
	return strings.HasPrefix(addr, "12D3") ||
		strings.HasPrefix(addr, "Qm") ||
		strings.HasPrefix(addr, "16Ui")
}

func containsWallet(list []string, wallet string) bool {
	for _, w := range list {
		if w == wallet {
			return true
		}
	}
	return false
}
