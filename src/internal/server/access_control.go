package server

import (
	"net/http"
	"opentela/internal/common"
	"opentela/internal/protocol"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

const routingScopeContextKey = "routing_scope"

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
func resolveCallerWallet(r *http.Request, controlPlaneWallet string, controlPlaneAuthoritative bool) string {
	if controlPlaneAuthoritative {
		return controlPlaneWallet
	}

	if !isLibp2pRemoteAddr(r.RemoteAddr) {
		return ""
	}

	if clientWallet := r.Header.Get("X-Otela-Client-Wallet"); clientWallet != "" {
		return clientWallet
	}

	addr := r.RemoteAddr
	peer, err := protocol.GetPeerFromTable(addr)
	if err != nil {
		return ""
	}
	if peer.TrustLevel >= protocol.TrustSelfAttested && peer.IdentityAttestation != nil {
		return peer.IdentityAttestation.WalletPubkey
	}
	return peer.Owner
}

func accessControlMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, hasScope, err := maybeWorkerScopeFromContext(c)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		controlPlaneWallet := ""
		controlPlaneAuthoritative := false
		if hasScope {
			if service, resolveErr := resolveUniqueLocalService(scope.Service); writeServiceResolutionError(c, resolveErr) {
				c.Abort()
				return
			} else {
				c.Set("resolved_local_service", service)
			}
			c.Set(routingScopeContextKey, scope)

			if scope.trusted() && !isControlPlaneEnabled() {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "trusted region routing requires control plane"})
				return
			}
			if scope.trusted() && !isLibp2pRemoteAddr(c.Request.RemoteAddr) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "trusted worker routes require a libp2p upstream"})
				return
			}

			if isControlPlaneEnabled() {
				upstreamPeerID := ""
				if isLibp2pRemoteAddr(c.Request.RemoteAddr) {
					upstreamPeerID = c.Request.RemoteAddr
				}
				decision, cpErr := authorizeRequestForScope(c.Request.Context(), c.GetHeader("Authorization"), scope, []string{protocol.MyID}, upstreamPeerID)
				if cpErr != nil {
					c.AbortWithStatusJSON(cpErr.Status, gin.H{"error": cpErr.clientMessage()})
					return
				}
				if decision == nil || !decision.allows(protocol.MyID) {
					c.AbortWithStatusJSON(statusForScopeDenial(scope, decision.reason(protocol.MyID)), gin.H{
						"error": messageForScopeDenial(scope, decision.reason(protocol.MyID)),
					})
					return
				}
				controlPlaneWallet = decision.PrimaryWallet
				controlPlaneAuthoritative = true
			}
		} else if isControlPlaneEnabled() {
			decision, cpErr := authorizeRequestForPeers(c.Request.Context(), c.GetHeader("Authorization"), []string{protocol.MyID})
			if cpErr != nil {
				c.AbortWithStatusJSON(cpErr.Status, gin.H{"error": cpErr.clientMessage()})
				return
			}
			if decision == nil || !decision.allows(protocol.MyID) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "access denied by instance ACL",
				})
				return
			}
			controlPlaneWallet = decision.PrimaryWallet
			controlPlaneAuthoritative = true
		}

		callerWallet := resolveCallerWallet(c.Request, controlPlaneWallet, controlPlaneAuthoritative)

		currentPolicy := strings.ToLower(viper.GetString("security.access_control.policy"))
		if currentPolicy == "" {
			currentPolicy = "any"
		}
		if currentPolicy == "any" {
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
				common.Logger.Warn("access_control: denied request (policy=self)")
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "access denied: only requests from the node operator's own wallet are accepted",
				})
				return
			}

		case "whitelist":
			allowed := viper.GetStringSlice("security.access_control.whitelist")
			if !containsWallet(allowed, callerWallet) {
				common.Logger.Warn("access_control: denied request (not in whitelist)")
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "access denied: wallet not in whitelist",
				})
				return
			}

		case "blacklist":
			blocked := viper.GetStringSlice("security.access_control.blacklist")
			if containsWallet(blocked, callerWallet) {
				common.Logger.Warn("access_control: denied request (blacklisted)")
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

func maybeWorkerScopeFromContext(c *gin.Context) (routingScope, bool, error) {
	serviceName := c.Param("service")
	region := c.Param("region")
	if strings.TrimSpace(serviceName) == "" {
		return routingScope{}, false, nil
	}
	partition := partitionPermissionless
	if strings.TrimSpace(region) != "" {
		partition = partitionTrustedRegion
	}
	scope, err := newRoutingScope(partition, region, routeKindWorker, serviceName)
	if err != nil {
		return routingScope{}, false, err
	}
	return scope, true, nil
}

func statusForScopeDenial(scope routingScope, reason string) int {
	switch reason {
	case "":
		return http.StatusForbidden
	case "untrusted_upstream", "service_context_required", "service_undeclared",
		"duplicate_service_name", "service_disabled", "partition_mismatch",
		"trusted_route_required", "wrong_role", "not_region_member",
		"membership_suspended", "membership_expired", "membership_revoked",
		"ownership_mismatch", "ownership_unavailable", "unmanaged":
		return http.StatusForbidden
	default:
		if scope.trusted() {
			return http.StatusForbidden
		}
		return http.StatusForbidden
	}
}

func messageForScopeDenial(scope routingScope, reason string) string {
	switch reason {
	case "service_context_required":
		return "service-managed targets require service-aware routing"
	case "service_undeclared":
		return "service is not declared for this instance"
	case "duplicate_service_name":
		return "duplicate local service name"
	case "service_disabled":
		return "service is disabled"
	case "partition_mismatch":
		return "service is not exposed through this partition"
	case "trusted_route_required":
		if scope.trusted() {
			return "trusted route required"
		}
		return "service must be reached through a trusted route"
	case "untrusted_upstream":
		return "upstream peer is not trusted for this region"
	default:
		return "access denied by instance ACL"
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
