package types

import (
	"context"
	"testing"
)

func TestApplyBuiltinAgentLocalizationOverlaysYAMLLocale(t *testing.T) {
	restore := OverrideBuiltinAgentEntriesForTest(map[string]*BuiltinAgentEntry{
		BuiltinQuickAnswerID: {
			ID:     BuiltinQuickAnswerID,
			Avatar: "quick.png",
			I18n: map[string]BuiltinAgentI18n{
				"default": {Name: "快速问答", Description: "中文 RAG"},
				"en-US":   {Name: "Quick Answer", Description: "Knowledge base RAG Q&A"},
				"zh-CN":   {Name: "快速问答", Description: "中文 RAG"},
			},
		},
	})
	t.Cleanup(restore)

	agent := &CustomAgent{
		ID:          BuiltinQuickAnswerID,
		Name:        "快速问答",
		Description: "中文 RAG",
		Avatar:      "",
		IsBuiltin:   true,
		TenantID:    1,
	}
	ctx := context.WithValue(context.Background(), LanguageContextKey, "en-US")
	ApplyBuiltinAgentLocalization(ctx, agent)

	if agent.Name != "Quick Answer" {
		t.Fatalf("Name = %q, want Quick Answer", agent.Name)
	}
	if agent.Description != "Knowledge base RAG Q&A" {
		t.Fatalf("Description = %q, want English copy", agent.Description)
	}
	if agent.Avatar != "quick.png" {
		t.Fatalf("Avatar = %q, want quick.png", agent.Avatar)
	}
}

func TestApplyBuiltinAgentLocalizationNilSafe(t *testing.T) {
	ApplyBuiltinAgentLocalization(context.Background(), nil)
}

func TestApplyBuiltinAgentLocalizationLeavesUnknownAgents(t *testing.T) {
	agent := &CustomAgent{ID: "custom-agent-1", Name: "Mine", Description: "keep me"}
	ctx := context.WithValue(context.Background(), LanguageContextKey, "en-US")
	ApplyBuiltinAgentLocalization(ctx, agent)
	if agent.Name != "Mine" || agent.Description != "keep me" {
		t.Fatalf("custom agent was rewritten: %+v", agent)
	}
}

func TestBuiltinAgentSelectableDefaultsToTrue(t *testing.T) {
	restore := OverrideBuiltinAgentEntriesForTest(map[string]*BuiltinAgentEntry{
		"legacy": {ID: "legacy"},
	})
	defer restore()

	if !IsBuiltinAgentSelectable("legacy") {
		t.Fatal("omitted selectable must remain enabled for backward compatibility")
	}
	if !IsBuiltinAgentSelectable("hard-coded-fallback") {
		t.Fatal("unconfigured builtins must remain selectable")
	}
}

func TestBuiltinAgentSelectableCanHideWithoutRemovingDefinition(t *testing.T) {
	disabled := false
	restore := OverrideBuiltinAgentEntriesForTest(map[string]*BuiltinAgentEntry{
		BuiltinQuickAnswerID: {ID: BuiltinQuickAnswerID, Selectable: &disabled},
	})
	defer restore()

	if IsBuiltinAgentSelectable(BuiltinQuickAnswerID) {
		t.Fatal("explicit selectable=false must hide the builtin from lists")
	}
	for _, id := range GetSelectableBuiltinAgentIDs() {
		if id == BuiltinQuickAnswerID {
			t.Fatal("hidden builtin must not be returned by the user-facing builtin list")
		}
	}
	if agent := GetBuiltinAgentWithContext(context.Background(), BuiltinQuickAnswerID, 7); agent == nil {
		t.Fatal("hidden builtin definition must remain resolvable for historical sessions")
	}
}
