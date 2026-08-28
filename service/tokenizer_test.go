package service

import (
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

func TestCanonicalTokenizerModelGPT56(t *testing.T) {
	for _, model := range []string{
		"gpt-5.6-sol",
		"gpt-5.6-terra",
		"gpt-5.6-luna",
		"gpt-5.6-sol-openai-compact",
	} {
		if got := canonicalTokenizerModel(model); got != "gpt-5" {
			t.Fatalf("canonicalTokenizerModel(%q) = %q, want gpt-5", model, got)
		}
		if _, err := tokenizer.ForModel(tokenizer.Model(canonicalTokenizerModel(model))); err != nil {
			t.Fatalf("ForModel(gpt-5) failed for %q: %v", model, err)
		}
	}
}

func TestGPT56TokenizerIsNotCl100kFallback(t *testing.T) {
	InitTokenEncoders()
	got := getTokenEncoder("gpt-5.6-sol")
	want, err := tokenizer.ForModel(tokenizer.Model("gpt-5"))
	if err != nil {
		t.Fatalf("ForModel(gpt-5): %v", err)
	}
	text := "お誕生日おめでとう"
	gotCount, _ := got.Count(text)
	wantCount, _ := want.Count(text)
	if gotCount != wantCount {
		t.Fatalf("gpt-5.6-sol token count = %d, gpt-5 count = %d", gotCount, wantCount)
	}
}
