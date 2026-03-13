package swim

import "github.com/prometheus/client_golang/prometheus"

var (
	swimProbeDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "otela_swim_probe_duration_seconds",
			Help:    "SWIM probe round-trip time",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"result"},
	)
	swimProbeTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "otela_swim_probe_total",
			Help: "SWIM probe outcomes",
		},
		[]string{"result"},
	)
	swimMemberCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "otela_swim_member_count",
			Help: "Current SWIM membership view",
		},
		[]string{"status"},
	)
)

func init() {
	prometheus.MustRegister(swimProbeDuration, swimProbeTotal, swimMemberCount)
}
