package solana

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// Rate defines a provider's pricing for a specific service and metric.
type Rate struct {
	ProviderAddress string // Solana wallet address
	Service         string // e.g. "llm", "sandbox"
	MetricName      string // e.g. "tokens", "gpu_ms"
	PricePerUnit    int64  // OTELA base units (9 decimals)
}

// RateManager stores and retrieves provider rates.
type RateManager struct {
	mu    sync.RWMutex
	rates map[string]Rate // key: "provider:service:metric"

	// Fallback defaults when a provider has no explicit rate.
	defaultTokenRate int64
	defaultGPUMsRate int64
}

// NewRateManager creates a RateManager with the given default rates.
func NewRateManager(defaultTokenRate, defaultGPUMsRate int64) *RateManager {
	return &RateManager{
		rates:            make(map[string]Rate),
		defaultTokenRate: defaultTokenRate,
		defaultGPUMsRate: defaultGPUMsRate,
	}
}

func rateKey(provider, service, metric string) string {
	return provider + ":" + service + ":" + metric
}

// GetRate returns the rate for a provider/service/metric combination.
// Falls back to default rates if no explicit rate is configured.
func (rm *RateManager) GetRate(provider, service, metric string) (Rate, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if r, ok := rm.rates[rateKey(provider, service, metric)]; ok {
		return r, nil
	}

	// Fall back to default rates by metric name.
	var defaultPrice int64
	switch metric {
	case "tokens":
		defaultPrice = rm.defaultTokenRate
	case "gpu_ms":
		defaultPrice = rm.defaultGPUMsRate
	default:
		return Rate{}, fmt.Errorf("no rate found for %s/%s/%s and no default for metric %q", provider, service, metric, metric)
	}

	return Rate{
		ProviderAddress: provider,
		Service:         service,
		MetricName:      metric,
		PricePerUnit:    defaultPrice,
	}, nil
}

// SetRate stores a rate for a provider/service/metric combination.
func (rm *RateManager) SetRate(r Rate) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.rates[rateKey(r.ProviderAddress, r.Service, r.MetricName)] = r
}

// LoadFromConfig reads provider rates from a YAML file.
func (rm *RateManager) LoadFromConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read rates config: %w", err)
	}

	var cfg ratesConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse rates config: %w", err)
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	for _, p := range cfg.Providers {
		for _, s := range p.Services {
			for _, m := range s.Metrics {
				r := Rate{
					ProviderAddress: p.Address,
					Service:         s.Name,
					MetricName:      m.Name,
					PricePerUnit:    m.PricePer1000 / 1000, // convert to per-unit
				}
				if m.PricePer1000 > 0 && m.PricePer1000 < 1000 {
					// Sub-unit pricing: keep at least 1 base unit per unit.
					r.PricePerUnit = 1
				}
				rm.rates[rateKey(r.ProviderAddress, r.Service, r.MetricName)] = r
			}
		}
	}

	return nil
}

// ratesConfig mirrors the YAML structure in rates.yaml.
type ratesConfig struct {
	Providers []struct {
		Address  string `yaml:"address"`
		Services []struct {
			Name    string `yaml:"name"`
			Metrics []struct {
				Name         string `yaml:"name"`
				PricePer1000 int64  `yaml:"price_per_1000"`
			} `yaml:"metrics"`
		} `yaml:"services"`
	} `yaml:"providers"`
}
