package cmd

type P2PConfig struct {
	Port string `json:"port" yaml:"port"`
}

type VaccumConfig struct {
	Interval int `json:"interval" yaml:"interval"`
}

type QueueConfig struct {
	Port string `json:"port" yaml:"port"`
}

type Config struct {
	Path    string        `json:"path" yaml:"path"`
	Port    string        `json:"port" yaml:"port"`
	Name    string        `json:"name" yaml:"name"`
	P2p     P2PConfig     `json:"p2p" yaml:"p2p"`
	Vacuum  VaccumConfig  `json:"vacuum" yaml:"vacuum"`
	Queue   QueueConfig   `json:"queue" yaml:"queue"`
	Account AccountConfig `json:"account" yaml:"account"`
	Solana  SolanaConfig  `json:"solana" yaml:"solana"`
	Seed    string        `json:"seed" yaml:"seed"`
	TCPPort string        `json:"tcp_port" yaml:"tcp_port"`
	UDPPort string        `json:"udp_port" yaml:"udp_port"`
}

type AccountConfig struct {
	Wallet string `json:"wallet" yaml:"wallet"`
}

type SolanaConfig struct {
	RPC              string `json:"rpc" yaml:"rpc"`
	Mint             string `json:"mint" yaml:"mint"`
	SkipVerification bool   `json:"skip_verification" yaml:"skip_verification"`
}

// configDirName is the canonical subdirectory under $HOME/.config that
// holds the OpenTela node configuration file (cfg.yaml).  Wallet data
// lives at the same path so both the config and the wallet share the
// ~/.config/opentela root.
const configDirName = "opentela"

// legacyConfigDirName is the old directory name so we can detect and
// transparently migrate existing installations.
const legacyConfigDirName = "ocf"

var defaultConfig = Config{
	Seed:    "0",
	Path:    "",
	Port:    "8092",
	Name:    "relay",
	TCPPort: "43905",
	UDPPort: "59820",
	P2p:     P2PConfig{Port: "8093"},
	Vacuum:  VaccumConfig{Interval: 10},
	Queue:   QueueConfig{Port: "8094"},
	Account: AccountConfig{Wallet: ""},
	Solana: SolanaConfig{
		RPC:              "https://api.mainnet-beta.solana.com",
		Mint:             "EsmcTrdLkFqV3mv4CjLF3AmCx132ixfFSYYRWD78cDzR",
		SkipVerification: false,
	},
}
