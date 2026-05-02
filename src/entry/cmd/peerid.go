package cmd

import (
	"fmt"
	"os"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spf13/cobra"
	"opentela/internal/protocol"
)

var peerIDCmd = &cobra.Command{
	Use:   "peer-id",
	Short: "Print the local libp2p PeerID",
	RunE: func(cmd *cobra.Command, args []string) error {
		keyPath, err := protocol.ResolveKeyPath()
		if err != nil {
			return fmt.Errorf("resolve key path: %w", err)
		}
		data, err := os.ReadFile(keyPath)
		if err != nil {
			return fmt.Errorf("read key %s: %w", keyPath, err)
		}
		priv, err := crypto.UnmarshalPrivateKey(data)
		if err != nil {
			return fmt.Errorf("unmarshal key: %w", err)
		}
		pid, err := peer.IDFromPublicKey(priv.GetPublic())
		if err != nil {
			return fmt.Errorf("derive peer id: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), pid.String())
		return nil
	},
}
