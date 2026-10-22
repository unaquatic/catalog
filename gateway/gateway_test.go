package gateway

import (
	"errors"
	"testing"
)

func TestPriceAt(t *testing.T) {
	p := Price{Rates: Rates{Input: 2}, Tiers: []PriceTier{
		{Above: 272_000, Rates: Rates{Input: 4}},
		{Above: 1_000_000, Rates: Rates{Input: 8}},
	}}
	for prompt, want := range map[int]float64{0: 2, 272_000: 2, 272_001: 4, 1_000_001: 8} {
		if got := p.At(prompt).Input; got != want {
			t.Errorf("At(%d).Input = %v, want %v", prompt, got, want)
		}
	}
}

func TestDecodeVersion(t *testing.T) {
	if _, err := Decode([]byte(`{"version": 1, "providers": {}, "models": {}}`)); !errors.Is(err, ErrVersion) {
		t.Errorf("Decode(version 1) = %v, want ErrVersion", err)
	}
}
