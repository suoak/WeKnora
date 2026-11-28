package config

import (
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestWikiCatalogFallbackPresetAndRuntimePrompt(t *testing.T) {
	var builtin struct {
		Entries []types.BuiltinAgentEntry `yaml:"builtin_agents"`
	}
	data, err := os.ReadFile("../../config/builtin_agents.yaml")
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &builtin))
	var configured *types.CustomAgentConfig
	for i := range builtin.Entries {
		if builtin.Entries[i].ID == types.BuiltinWikiResearcherID {
			configured = &builtin.Entries[i].Config
		}
	}
	require.NotNil(t, configured)

	var presets struct {
		Entries []types.AgentTypePresetEntry `yaml:"agent_type_presets"`
	}
	data, err = os.ReadFile("../../config/agent_type_presets.yaml")
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &presets))
	var editor *types.AgentTypePresetConfig
	for _, entry := range presets.Entries {
		if entry.ID == types.AgentTypeWikiQA {
			editor = entry.Config
		}
	}
	require.NotNil(t, editor)
	require.ElementsMatch(t, configured.AllowedTools, editor.AllowedTools)
	for _, name := range []string{"wiki_search", "wiki_read_page", "read_document", "list_documents"} {
		require.Contains(t, configured.AllowedTools, name)
	}
	require.NotContains(t, configured.AllowedTools, "search_knowledge")
	require.NotContains(t, configured.AllowedTools, "wiki_write_page")
	require.Equal(t, 30, configured.MaxIterations)
	require.Equal(t, configured.MaxIterations, editor.MaxIterations)

	templates, err := loadPromptTemplates("../../config")
	require.NoError(t, err)
	cfg := &Config{PromptTemplates: templates}
	resolved, _ := cfg.ResolveCustomAgentPrompts(&types.CustomAgent{Config: *configured})
	require.Contains(t, resolved, "list_documents")
	require.Contains(t, resolved, "original-document fallback")
	require.Contains(t, resolved, "never invent handles or broaden that scope")
	require.Equal(t, "wiki_researcher", configured.SystemPromptID)
}
