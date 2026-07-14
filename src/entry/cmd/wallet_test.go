package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestBalanceCommandFlags(t *testing.T) {
	for _, cmd := range []*cobra.Command{balanceCmd, walletBalanceCmd} {
		flags := cmd.Flags()

		assert.NotNil(t, flags.Lookup("solana.rpc"))
		mintFlag := flags.Lookup("solana.mint")
		if assert.NotNil(t, mintFlag) {
			assert.Equal(t, defaultConfig.Solana.Mint, mintFlag.DefValue)
		}
	}
}
