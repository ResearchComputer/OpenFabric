package server

import (
	"encoding/base64"
	"encoding/hex"
	"net"

	"opentela/internal/protocol"

	"github.com/gin-gonic/gin"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
)

func isLoopback(c *gin.Context) bool {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// getSelf returns this node's Peer struct. Localhost only.
func getSelf(c *gin.Context) {
	if !isLoopback(c) {
		c.JSON(403, gin.H{"error": "localhost only"})
		return
	}
	c.JSON(200, protocol.GetSelf())
}

// signData signs data with this node's libp2p private key. Localhost only.
// Returns the signature and the marshalled public key (needed for RSA peer ID verification).
func signData(c *gin.Context) {
	if !isLoopback(c) {
		c.JSON(403, gin.H{"error": "localhost only"})
		return
	}
	var req struct {
		Data string `json:"data"` // hex-encoded
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	dataBytes, err := hex.DecodeString(req.Data)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid hex data"})
		return
	}
	host, _ := protocol.GetP2PNode(nil)
	if host == nil {
		c.JSON(503, gin.H{"error": "node not ready"})
		return
	}
	privKey := host.Peerstore().PrivKey(host.ID())
	if privKey == nil {
		c.JSON(500, gin.H{"error": "no private key available"})
		return
	}
	sig, err := privKey.Sign(dataBytes)
	if err != nil {
		c.JSON(500, gin.H{"error": "signing failed: " + err.Error()})
		return
	}
	pubKeyBytes, err := libp2pcrypto.MarshalPublicKey(privKey.GetPublic())
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to marshal public key"})
		return
	}
	c.JSON(200, gin.H{
		"signature":  hex.EncodeToString(sig),
		"public_key": base64.StdEncoding.EncodeToString(pubKeyBytes),
	})
}
