package llm

import "testing"

func TestResolveUsagePreservesCacheInclusiveFallbackTotal(t *testing.T) {
	usage := resolveUsage([]byte(`{"usage":{"input_tokens":120,"output_tokens":30,"cache_read_input_tokens":40,"cache_creation_input_tokens":10}}`))

	if usage == nil {
		t.Fatal("expected usage")
	}
	if usage.TotalTokens != 200 {
		t.Fatalf("total tokens = %d, want 200", usage.TotalTokens)
	}
}

func TestResolveUsageCostPaths(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want float64
	}{
		{"claude envelope", `{"prompt_tokens":1,"total_cost_usd":0.01}`, 0.01},
		{"flat", `{"prompt_tokens":1,"cost_usd":0.02}`, 0.02},
		{"usage", `{"prompt_tokens":1,"usage":{"cost_usd":0.03}}`, 0.03},
		{"wrapped", `{"prompt_tokens":1,"data":{"cost_usd":0.04}}`, 0.04},
		{"negative rejected", `{"prompt_tokens":1,"cost_usd":-1}`, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage := resolveUsage([]byte(test.raw))
			if usage == nil || usage.CostUSD != test.want {
				t.Fatalf("usage = %#v, want cost %f", usage, test.want)
			}
		})
	}
}
