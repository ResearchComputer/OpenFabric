package analytics

import (
	"math"
	"math/rand"
	"sort"
)

// reservoir keeps an unbiased bounded sample of float64 observations using
// Algorithm R, so percentile/mean memory stays capped regardless of volume.
type reservoir struct {
	max     int
	samples []float64
	count   int
	rng     *rand.Rand
}

func newReservoir(max int, rng *rand.Rand) *reservoir {
	if max < 1 {
		max = 1
	}
	return &reservoir{max: max, samples: make([]float64, 0, max), rng: rng}
}

func (r *reservoir) add(v float64) {
	r.count++
	if len(r.samples) < r.max {
		r.samples = append(r.samples, v)
		return
	}
	j := r.rng.Intn(r.count)
	if j < r.max {
		r.samples[j] = v
	}
}

func (r *reservoir) len() int { return len(r.samples) }

func (r *reservoir) mean() float64 {
	if len(r.samples) == 0 {
		return 0
	}
	var sum float64
	for _, v := range r.samples {
		sum += v
	}
	return sum / float64(len(r.samples))
}

// percentile returns the p-th percentile (0..100) using nearest-rank on a
// sorted copy of the current samples. Returns 0 for an empty reservoir.
func (r *reservoir) percentile(p float64) float64 {
	n := len(r.samples)
	if n == 0 {
		return 0
	}
	sorted := make([]float64, n)
	copy(sorted, r.samples)
	sort.Float64s(sorted)
	rank := int(math.Ceil(p/100.0*float64(n))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= n {
		rank = n - 1
	}
	return sorted[rank]
}
