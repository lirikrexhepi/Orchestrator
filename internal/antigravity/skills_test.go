package antigravity

import (
	"context"
	"strings"
	"testing"

	"composer/internal/domain"
	"composer/internal/provider"
)

func TestSkillPolicyInSystemPromptAppend(t *testing.T) {
	// Case 1: Skills provided
	{
		srv := &fakeServer{}
		s := &session{
			threadID:  "t-skills-1",
			tools:     make(map[string]bool),
			baselines: make(map[string]string),
			pending:   make(map[string]*pendingApproval),
		}
		a, _, _, _ := setupFakeServerPair(t, s, srv)

		skills := &domain.SkillPolicy{
			Mode:   domain.SkillsOnly,
			Skills: []domain.SkillRef{{Name: "spacious-minimal", Path: `C:\skills\spacious-minimal`}},
		}

		in := domain.SessionStartInput{
			ThreadID: "t-skills-1",
			Skills:   skills,
		}

		if err := a.initSession(context.Background(), s, in); err != nil {
			t.Fatalf("initSession failed: %v", err)
		}

		srv.mu.Lock()
		meta := srv.newParams.Meta
		srv.mu.Unlock()

		if meta == nil {
			t.Fatal("expected _meta in session/new params")
		}
		agyMeta := meta["antigravity"].(map[string]any)
		spMeta := agyMeta["systemPrompt"].(map[string]any)
		appendPrompt := spMeta["append"].(string)

		if !strings.Contains(appendPrompt, provider.RuntimeInstructions) {
			t.Errorf("systemPrompt.append missing RuntimeInstructions: %q", appendPrompt)
		}
		if !strings.Contains(appendPrompt, "spacious-minimal") {
			t.Errorf("systemPrompt.append missing skill name: %q", appendPrompt)
		}
		if !strings.Contains(appendPrompt, `C:\skills\spacious-minimal`) {
			t.Errorf("systemPrompt.append missing skill path: %q", appendPrompt)
		}
	}

	// Case 2: SkillsNone
	{
		srv := &fakeServer{}
		s := &session{
			threadID:  "t-skills-none",
			tools:     make(map[string]bool),
			baselines: make(map[string]string),
			pending:   make(map[string]*pendingApproval),
		}
		a, _, _, _ := setupFakeServerPair(t, s, srv)

		in := domain.SessionStartInput{
			ThreadID: "t-skills-none",
			Skills:   &domain.SkillPolicy{Mode: domain.SkillsNone},
		}

		if err := a.initSession(context.Background(), s, in); err != nil {
			t.Fatalf("initSession failed: %v", err)
		}

		srv.mu.Lock()
		meta := srv.newParams.Meta
		srv.mu.Unlock()

		if meta == nil {
			t.Fatal("expected _meta in session/new params")
		}
		agyMeta := meta["antigravity"].(map[string]any)
		spMeta := agyMeta["systemPrompt"].(map[string]any)
		appendPrompt := spMeta["append"].(string)

		if !strings.Contains(appendPrompt, provider.RuntimeInstructions) {
			t.Errorf("systemPrompt.append missing RuntimeInstructions: %q", appendPrompt)
		}
		if strings.Contains(appendPrompt, "Before you start") {
			t.Errorf("SkillsNone must not append skill instructions: %q", appendPrompt)
		}
	}
}
