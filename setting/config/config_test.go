package config

import "testing"

func TestUpdateConfigFromMapReplacesMapValues(t *testing.T) {
	type sampleConfig struct {
		Discount map[int]float64 `json:"discount"`
	}

	cfg := &sampleConfig{
		Discount: map[int]float64{
			10: 0.95,
			20: 0.90,
		},
	}

	err := UpdateConfigFromMap(cfg, map[string]string{
		"discount": `{}`,
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap returned error: %v", err)
	}

	if cfg.Discount == nil {
		t.Fatalf("discount map should decode to an empty map, got nil")
	}

	if len(cfg.Discount) != 0 {
		t.Fatalf("discount map should be empty after updating with {}, got %#v", cfg.Discount)
	}
}
