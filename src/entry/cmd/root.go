package cmd

import (
	"fmt"
	"opentela/internal/common"
	"os"
	"path"
	"strconv"
	"strings"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var cfgFile string
var rootcmd = &cobra.Command{
	Use:   "otela",
	Short: "OpenTela is a decentralized fabric for running machine learning applications.",
	Long:  ``,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return initConfig(cmd)
	},
	Run: func(cmd *cobra.Command, args []string) {
		err := cmd.Help()
		if err != nil {
			common.Logger.Error("Could not print help", "error", err)
		}
	},
}

//nolint:gochecknoinits
func init() {
	rootcmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/opentela/cfg.yaml)")
	startCmd.Flags().String("wallet.account", "", "wallet account")
	startCmd.Flags().String("account.wallet", "", "path to wallet key file")
	startCmd.Flags().String("bootstrap.addr", "", "bootstrap address")
	startCmd.Flags().StringSlice("bootstrap.source", nil, "bootstrap source (HTTP URL, dnsaddr://host, or multiaddr). Repeatable")
	startCmd.Flags().StringSlice("bootstrap.static", []string{
		"https://bootstraps.opentela.ai/v1/dnt/bootstraps",
		"http://140.238.223.116:8092/v1/dnt/bootstraps",
		"http://152.67.64.117:8092/v1/dnt/bootstraps",
	}, "static bootstrap sources (HTTP URL, dnsaddr://, or multiaddr). Repeatable")
	startCmd.Flags().String("seed", "0", "Seed")
	startCmd.Flags().String("mode", "node", "Mode (standalone, local, full)")
	startCmd.Flags().String("tcpport", "43905", "TCP Port")
	startCmd.Flags().String("udpport", "59820", "UDP Port")
	startCmd.Flags().String("subprocess", "", "Subprocess to start")
	startCmd.Flags().String("public-addr", "", "Public address if you have one (by setting this, you can be a bootstrap node)")
	startCmd.Flags().String("service.name", "", "Service name")
	startCmd.Flags().String("service.port", "", "Service port")
	startCmd.Flags().String("solana.rpc", defaultConfig.Solana.RPC, "Solana RPC endpoint")
	startCmd.Flags().String("solana.mint", defaultConfig.Solana.Mint, "SPL token mint to verify ownership")
	startCmd.Flags().Bool("solana.skip_verification", defaultConfig.Solana.SkipVerification, "Skip Solana token ownership verification (use for testing only)")
	startCmd.Flags().Bool("cleanslate", true, "Clean slate")
	startCmd.Flags().String("role", "worker", "Node role (worker, head, relay)")
	rootcmd.AddCommand(initCmd)
	rootcmd.AddCommand(startCmd)
	rootcmd.AddCommand(versionCmd)
	rootcmd.AddCommand(updateCmd)
	rootcmd.AddCommand(walletCmd)
}

// configFilePath returns the canonical path for the OpenTela config file.
// It first checks for the new ~/.config/opentela/cfg.yaml location; if that
// does not exist but the legacy ~/.config/ocf/cfg.yaml does, the legacy path
// is returned so the existing installation keeps working until the user
// migrates.
func configFilePath(home string) string {
	newPath := path.Join(home, ".config", configDirName, "cfg.yaml")
	if _, err := os.Stat(newPath); err == nil {
		return newPath
	}

	legacyPath := path.Join(home, ".config", legacyConfigDirName, "cfg.yaml")
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath
	}

	// Neither exists yet — use the new location.
	return newPath
}

func initConfig(cmd *cobra.Command) error {
	var home string
	var err error
	viper.SetEnvPrefix("of")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	viper.SetDefault("crdt.tombstone_retention", "24h")
	viper.SetDefault("crdt.tombstone_compaction_interval", "1h")
	viper.SetDefault("crdt.tombstone_compaction_batch", 512)

	// Add billing configuration defaults (opt-in, disabled by default)
	viper.SetDefault("billing.enabled", false)
	viper.SetDefault("billing.value_threshold", 10000000) // lamports
	viper.SetDefault("billing.max_interval_minutes", 60)
	viper.SetDefault("billing.dispute_threshold_pct", 10)

	viper.SetDefault("role", "worker")
	viper.SetDefault("security.require_signed_binary", true)

	// Metrics aggregation configuration (opt-in, disabled by default)
	viper.SetDefault("metrics.aggregation_enabled", false)
	viper.SetDefault("metrics.scrape_interval_seconds", 30)
	viper.SetDefault("metrics.scrape_timeout_seconds", 5)
	viper.SetDefault("metrics.worker_metrics_path", "/metrics")
	viper.SetDefault("metrics.max_concurrent_scrapes", 10)

	// SWIM membership protocol parameters
	viper.SetDefault("swim.probe_interval", "500ms")
	viper.SetDefault("swim.probe_timeout", "500ms")
	viper.SetDefault("swim.indirect_probe_timeout", "1s")
	viper.SetDefault("swim.indirect_probes", 3)
	viper.SetDefault("swim.suspect_timeout", "5s")
	viper.SetDefault("swim.retransmit_mult", 3)
	viper.SetDefault("swim.metadata_max_bytes", 256)

	// Production logging mode: reduces log volume via sampling (opt-in)
	viper.SetDefault("production_logging", false)

	// Scalability feature flags (all default to false for safe rollout)
	viper.SetDefault("scalability.swim_enabled", false)
	viper.SetDefault("scalability.crdt_tuned", false)
	viper.SetDefault("scalability.weighted_routing", false)
	viper.SetDefault("scalability.admission_control", false)
	viper.SetDefault("scalability.expected_workers", 0) // 0 = auto (disabled)

	// CRDT tuned values (used when scalability.crdt_tuned=true)
	viper.SetDefault("crdt.tuned_gossipsub_d", 10)
	viper.SetDefault("crdt.tuned_gossipsub_dlo", 4)
	viper.SetDefault("crdt.tuned_gossipsub_dhi", 16)
	viper.SetDefault("crdt.tuned_rebroadcast_interval", "60s")
	viper.SetDefault("crdt.tuned_workers", 16)

	// Don't forget to read config either from cfgFile or from home directory!
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
		// print out the config file
		common.Logger.Debug("Using config file: ", viper.ConfigFileUsed())
	} else {
		// Find home directory.
		home, err = homedir.Dir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		viper.SetConfigFile(configFilePath(home))
	}
	if err = viper.ReadInConfig(); err != nil {
		viper.SetDefault("path", defaultConfig.Path)
		viper.SetDefault("port", defaultConfig.Port)
		viper.SetDefault("name", defaultConfig.Name)
		viper.SetDefault("p2p", defaultConfig.P2p)
		viper.SetDefault("tcpport", defaultConfig.TCPPort)
		viper.SetDefault("udpport", defaultConfig.UDPPort)
		viper.SetDefault("vacuum.interval", defaultConfig.Vacuum.Interval)
		viper.SetDefault("queue.port", defaultConfig.Queue.Port)
		viper.SetDefault("account.wallet", defaultConfig.Account.Wallet)
		viper.SetDefault("wallet.account", "")
		viper.SetDefault("solana.rpc", defaultConfig.Solana.RPC)
		viper.SetDefault("solana.mint", defaultConfig.Solana.Mint)
		viper.SetDefault("solana.skip_verification", defaultConfig.Solana.SkipVerification)
		configPath := path.Join(home, ".config", configDirName, "cfg.yaml")
		err = os.MkdirAll(path.Dir(configPath), os.ModePerm)
		if err != nil {
			common.Logger.Error("Could not create config directory", "error", err)
			os.Exit(1)
		}

		if err = viper.SafeWriteConfigAs(configPath); err != nil {
			if os.IsNotExist(err) {
				err = viper.WriteConfigAs(configPath)
				if err != nil {
					common.Logger.Warn("Cannot write config file", "error", err)
				}
			}
		}
	}
	// Bind each Cobra Flag to its associated Viper Key
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Changed || !viper.IsSet(flag.Name) {
			switch flag.Value.Type() {
			case "bool":
				value, err := strconv.ParseBool(flag.Value.String())
				if err != nil {
					viper.Set(flag.Name, flag.Value)
				} else {
					viper.Set(flag.Name, value)
				}
			case "int":
				value, err := strconv.ParseInt(flag.Value.String(), 0, 64)
				if err != nil {
					viper.Set(flag.Name, flag.Value)
				} else {
					viper.Set(flag.Name, value)
				}
			case "stringSlice", "stringArray":
				if sliceValue, ok := flag.Value.(pflag.SliceValue); ok {
					viper.Set(flag.Name, sliceValue.GetSlice())
				} else {
					viper.Set(flag.Name, strings.Split(flag.Value.String(), ","))
				}
			default:
				viper.Set(flag.Name, flag.Value)
			}
		}
	})
	common.InitLogger()
	return nil
}

func Execute() {
	if err := rootcmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
