package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
)

func TestFilterCompactVirtualModels(t *testing.T) {
	models := []dto.OpenAIModels{
		{Id: "gpt-5.4"},
		{Id: "gpt-5.4-openai-compact"},
		{Id: "gpt-5.3-codex"},
		{Id: "gpt-5.3-codex-openai-compact"},
	}

	filtered := filterCompactVirtualModels(models)

	assert.Equal(t, []dto.OpenAIModels{
		{Id: "gpt-5.4"},
		{Id: "gpt-5.3-codex"},
	}, filtered)
}
