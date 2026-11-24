package claude

import (
	"testing"

	"composer/internal/domain"
)

func modelByID(models []domain.Model, id string) *domain.Model {
	for i := range models {
		if models[i].ID == id {
			return &models[i]
		}
	}
	return nil
}

func effortDefault(model *domain.Model) string {
	for _, option := range model.Options {
		if option.ID == domain.OptionEffort {
			if def, ok := option.Default.(string); ok {
				return def
			}
			return ""
		}
	}
	return ""
}

func TestSonnet55GatedOnCLIVersion(t *testing.T) {
	current := Models("2.1.284")
	found := modelByID(current, "claude-sonnet-5-5")
	if found == nil {
		t.Fatalf("claude-sonnet-5-5 missing on CLI 2.1.284: %+v", current)
	}
	if effortDefault(found) != "medium" {
		t.Fatalf("sonnet 5.5 default effort = %q, want medium", effortDefault(found))
	}
	if found.Default {
		t.Fatalf("sonnet 5.5 must not steal the default from opus 5.5")
	}

	previous := Models("2.1.283")
	if modelByID(previous, "claude-sonnet-5-5") != nil {
		t.Fatalf("claude-sonnet-5-5 must stay hidden on CLI 2.1.283")
	}
	if modelByID(previous, "claude-sonnet-5") == nil {
		t.Fatalf("claude-sonnet-5 fallback missing on CLI 2.1.283")
	}
}

func TestOpus55DefaultWithFallback(t *testing.T) {
	current := Models("2.1.284")
	if model := modelByID(current, "claude-opus-5-5"); model == nil || !model.Default {
		t.Fatalf("opus 5.5 should be the default on current CLIs")
	}

	old := Models("2.1.219")
	if modelByID(old, "claude-opus-5-5") != nil {
		t.Fatalf("opus 5.5 must stay hidden on CLI 2.1.219")
	}
	fallback := modelByID(old, "claude-opus-5")
	if fallback == nil || !fallback.Default {
		t.Fatalf("opus 5 should be the default fallback on CLI 2.1.219")
	}
}

func TestHaiku55GatedOnCLIVersion(t *testing.T) {
	current := Models("2.1.291")
	found := modelByID(current, "claude-haiku-5-5")
	if found == nil {
		t.Fatalf("claude-haiku-5-5 missing on CLI 2.1.291: %+v", current)
	}
	if found.Default {
		t.Fatalf("haiku 5.5 must not steal the default from opus 5.5")
	}
	if modelByID(Models("2.1.290"), "claude-haiku-5-5") != nil {
		t.Fatalf("claude-haiku-5-5 must stay hidden on CLI 2.1.290")
	}
	if modelByID(Models("2.1.290"), "claude-haiku-4-5") == nil {
		t.Fatalf("claude-haiku-4-5 must stay available on older CLIs")
	}
	hasContextWide := false
	for _, option := range found.Options {
		if option.ID == domain.OptionContextWindow {
			hasContextWide = true
			break
		}
	}
	if !hasContextWide {
		t.Fatalf("claude-haiku-5-5 should expose 1M context window option")
	}
}

func TestFable52GatedOnCLIVersion(t *testing.T) {
	current := Models("2.1.321")
	found := modelByID(current, "claude-fable-5-2")
	if found == nil {
		t.Fatalf("claude-fable-5-2 missing on CLI 2.1.321: %+v", current)
	}
	if effortDefault(found) != "medium" {
		t.Fatalf("fable 5.2 default effort = %q, want medium", effortDefault(found))
	}
	if found.Default {
		t.Fatalf("fable 5.2 must not steal the default from opus 5.5")
	}
	hasFastMode := false
	hasContextWide := false
	for _, option := range found.Options {
		if option.ID == domain.OptionFastMode {
			hasFastMode = true
		}
		if option.ID == domain.OptionContextWindow {
			hasContextWide = true
		}
	}
	if !hasFastMode {
		t.Fatalf("claude-fable-5-2 should expose fast mode option")
	}
	if !hasContextWide {
		t.Fatalf("claude-fable-5-2 should expose 1M context window option")
	}

	previous := Models("2.1.320")
	if modelByID(previous, "claude-fable-5-2") != nil {
		t.Fatalf("claude-fable-5-2 must stay hidden on CLI 2.1.320")
	}
	if modelByID(previous, "claude-fable-5-1") == nil {
		t.Fatalf("claude-fable-5-1 fallback missing on CLI 2.1.320")
	}
}
