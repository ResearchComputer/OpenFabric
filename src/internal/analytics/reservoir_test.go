package analytics

import (
	"math/rand"
	"testing"
)

func TestPercentileAndMean(t *testing.T) {
	r := newReservoir(100, rand.New(rand.NewSource(1)))
	for i := 1; i <= 100; i++ { // 1..100
		r.add(float64(i))
	}
	if got := r.percentile(50); got != 50 {
		t.Fatalf("p50 = %v, want 50", got)
	}
	if got := r.percentile(95); got != 95 {
		t.Fatalf("p95 = %v, want 95", got)
	}
	if got := r.mean(); got != 50.5 {
		t.Fatalf("mean = %v, want 50.5", got)
	}
}

func TestReservoirCapRespected(t *testing.T) {
	r := newReservoir(10, rand.New(rand.NewSource(42)))
	for i := 0; i < 1000; i++ {
		r.add(float64(i))
	}
	if r.len() != 10 {
		t.Fatalf("len = %d, want 10 (cap)", r.len())
	}
}

func TestPercentileEmpty(t *testing.T) {
	r := newReservoir(10, rand.New(rand.NewSource(1)))
	if r.percentile(50) != 0 || r.mean() != 0 {
		t.Fatal("empty reservoir should report 0")
	}
}
