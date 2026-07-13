package llm

import (
	"sort"
	"testing"
)

func TestLookupProvider_KnownProviders(t *testing.T) {
	names := []string{"anthropic", "openai", "dashscope"}
	for _, name := range names {
		p, ok := LookupProvider(name)
		if !ok {
			t.Errorf("LookupProvider(%q) returned false, want true", name)
			continue
		}
		if p.Name != name {
			t.Errorf("LookupProvider(%q).Name = %q", name, p.Name)
		}
		if p.Protocol == "" {
			t.Errorf("LookupProvider(%q).Protocol is empty", name)
		}
		if p.BaseURL == "" {
			t.Errorf("LookupProvider(%q).BaseURL is empty", name)
		}
		if len(p.Models) == 0 {
			t.Errorf("LookupProvider(%q).Models is empty", name)
		}
	}
}

func TestLookupProvider_Unknown(t *testing.T) {
	_, ok := LookupProvider("nonexistent-provider")
	if ok {
		t.Error("LookupProvider(nonexistent) returned true, want false")
	}
}

func TestListProviders_Order(t *testing.T) {
	providers := ListProviders()
	if len(providers) < 3 {
		t.Fatalf("expected at least 3 providers, got %d", len(providers))
	}
	expected := []string{"anthropic", "claude-code", "codex-code", "dashscope", "dashscope-tokenplan", "deepseek", "hy-tokenplan", "kimi", "mimo", "minimax", "openai", "tencent-tokenhub", "volcengine", "z-ai"}
	if len(providers) != len(expected) {
		t.Fatalf("expected %d providers, got %d", len(expected), len(providers))
	}
	for i, name := range expected {
		if providers[i].Name != name {
			t.Errorf("providers[%d].Name = %q, want %q", i, providers[i].Name, name)
		}
	}
}

func TestListProviders_ReturnsCopy(t *testing.T) {
	p1 := ListProviders()
	p1[0].Name = "mutated"

	p2 := ListProviders()
	if p2[0].Name == "mutated" {
		t.Error("ListProviders returns a reference to the registry, should return a copy")
	}
}

func TestLookupProvider_ReturnsCopyOfModels(t *testing.T) {
	p1, _ := LookupProvider("anthropic")
	p1.Models[0] = "mutated"

	p2, _ := LookupProvider("anthropic")
	if p2.Models[0] == "mutated" {
		t.Error("LookupProvider returns a reference to Models slice, should return a copy")
	}
}

func TestLookupProvider_PreservesModelOrder(t *testing.T) {
	p, ok := LookupProvider("anthropic")
	if !ok {
		t.Fatal("anthropic not found")
	}
	expected := []string{"claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6", "claude-sonnet-4-6"}
	if len(p.Models) != len(expected) {
		t.Fatalf("expected %d models, got %d", len(expected), len(p.Models))
	}
	for i, model := range expected {
		if p.Models[i] != model {
			t.Errorf("Models[%d] = %q, want %q", i, p.Models[i], model)
		}
	}
}

func TestListProviders_ReturnsSortedProviders(t *testing.T) {
	providers := ListProviders()
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("providers are not sorted: %v", names)
	}
}

func TestLookupProvider_AnthropicDetails(t *testing.T) {
	p, ok := LookupProvider("anthropic")
	if !ok {
		t.Fatal("anthropic not found")
	}
	if p.Protocol != "anthropic" {
		t.Errorf("Protocol = %q, want %q", p.Protocol, "anthropic")
	}
	if p.AuthHeader != "x-api-key" {
		t.Errorf("AuthHeader = %q, want %q", p.AuthHeader, "x-api-key")
	}
	if p.EnvVar != "ANTHROPIC_API_KEY" {
		t.Errorf("EnvVar = %q, want %q", p.EnvVar, "ANTHROPIC_API_KEY")
	}
}

func TestLookupProvider_OpenAIDetails(t *testing.T) {
	p, ok := LookupProvider("openai")
	if !ok {
		t.Fatal("openai not found")
	}
	if p.Protocol != "openai" {
		t.Errorf("Protocol = %q, want %q", p.Protocol, "openai")
	}
	if p.AuthHeader != "" {
		t.Errorf("AuthHeader = %q, want empty", p.AuthHeader)
	}
}

func TestLookupProvider_ClaudeCodeDetails(t *testing.T) {
	p, ok := LookupProvider("claude-code")
	if !ok {
		t.Fatal("claude-code not found")
	}
	if p.Protocol != "claude-code" {
		t.Errorf("Protocol = %q, want %q", p.Protocol, "claude-code")
	}
	if p.BaseURL != "" {
		t.Errorf("BaseURL = %q, want empty", p.BaseURL)
	}
	if p.AuthHeader != "" {
		t.Errorf("AuthHeader = %q, want empty", p.AuthHeader)
	}
	if p.EnvVar != "" {
		t.Errorf("EnvVar = %q, want empty", p.EnvVar)
	}
	if len(p.Models) == 0 || p.Models[0] != "claude-opus-4-8" {
		t.Errorf("Models = %v, want claude-opus-4-8 first", p.Models)
	}
}

func TestLookupProvider_CodexCodeDetails(t *testing.T) {
	p, ok := LookupProvider("codex-code")
	if !ok {
		t.Fatal("codex-code not found")
	}
	if p.Protocol != "codex-code" {
		t.Errorf("Protocol = %q, want %q", p.Protocol, "codex-code")
	}
	if p.BaseURL != "" || p.AuthHeader != "" || p.EnvVar != "" {
		t.Errorf("Codex CLI provider must not require HTTP configuration: %#v", p)
	}
	if len(p.Models) == 0 || p.Models[0] != "gpt-5.6-sol" {
		t.Errorf("Models = %v, want gpt-5.6-sol first", p.Models)
	}
}
