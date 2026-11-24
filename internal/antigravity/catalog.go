package antigravity

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"composer/internal/domain"
	"composer/internal/provider"
	"composer/internal/shell"
)

// agy bakes the effort tier into the model id (`gemini-3.7-flash-high`) rather
// than exposing a separate flag. Listed ids are therefore folded back into one
// model per family with an `effort` select, so the UI matches the other
// providers instead of showing a long flat list.
var effortSuffixes = []struct {
	suffix string
	id     string
	label  string
}{
	{"-xhigh", "xhigh", "Extra High"},
	{"-high", "high", "High"},
	{"-medium", "medium", "Medium"},
	{"-low", "low", "Low"},
	{"-max", "max", "Max"},
	{"-ultra", "ultra", "Ultra"},
}

type family struct {
	base     string
	name     string
	efforts  []domain.OptionChoice
	thinking bool
}

// ParseModels turns `agy models` output into grouped models. Each line is
// "<id>\t<display name>".
func ParseModels(stdout string) []domain.Model {
	order := make([]string, 0, 16)
	families := make(map[string]*family)

	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Fetching") {
			continue
		}

		id, display, _ := strings.Cut(line, "\t")
		id = strings.TrimSpace(id)
		display = strings.TrimSpace(display)
		if id == "" {
			continue
		}
		if display == "" {
			display = id
		}

		base, effort, label := splitEffort(id)
		name := baseDisplayName(display)

		entry, seen := families[base]
		if !seen {
			entry = &family{base: base, name: name}
			families[base] = entry
			order = append(order, base)
		}
		if strings.Contains(strings.ToLower(display), "thinking") {
			entry.thinking = true
		}
		if effort != "" {
			entry.efforts = append(entry.efforts, domain.OptionChoice{
				ID: effort, Label: label, Default: effort == "medium",
			})
		}
	}

	out := make([]domain.Model, 0, len(order))
	for _, base := range order {
		entry := families[base]
		model := domain.Model{ID: entry.base, DisplayName: entry.name}

		if len(entry.efforts) > 0 {
			sortEfforts(entry.efforts)
			ensureDefault(entry.efforts)
			model.Options = append(model.Options, domain.OptionDescriptor{
				ID: domain.OptionEffort, Label: "Reasoning", Type: domain.OptionSelect,
				Choices: entry.efforts, Default: defaultChoice(entry.efforts),
			})
		}
		out = append(out, model)
	}
	return out
}

var effortRank = map[string]int{
	"low": 0, "medium": 1, "high": 2, "xhigh": 3, "max": 4, "ultra": 5,
}

// sortEfforts orders tiers low→high; `agy models` lists them high-first.
func sortEfforts(choices []domain.OptionChoice) {
	sort.SliceStable(choices, func(i, j int) bool {
		return effortRank[choices[i].ID] < effortRank[choices[j].ID]
	})
}

// SplitModelSelection separates the base model id from its effort tier.
//
// agy carries the tier in two places — fused into listed ids
// (`gemini-3.7-flash-high`) and as a `--effort` flag — so a selection may hold
// it in either, and tiered families reject a bare `--model`. Every tiered model
// in the catalog advertises a default choice, so a UI-driven selection always
// supplies one; nothing is inferred from the id here.
func SplitModelSelection(model string, options domain.ModelOptions) (string, string) {
	base, suffixEffort, _ := splitEffort(model)

	if effort := options.String(domain.OptionEffort); effort != "" {
		return base, effort
	}
	return base, suffixEffort
}

func splitEffort(id string) (base, effort, label string) {
	for _, candidate := range effortSuffixes {
		if strings.HasSuffix(id, candidate.suffix) {
			return strings.TrimSuffix(id, candidate.suffix), candidate.id, candidate.label
		}
	}
	return id, "", ""
}

var effortParenTerms = []string{
	"(low)", "(medium)", "(high)", "(xhigh)", "(extra high)", "(max)", "(ultra)", "(thinking)",
}

func baseDisplayName(display string) string {
	idx := strings.Index(display, " (")
	if idx > 0 {
		paren := strings.ToLower(strings.TrimSpace(display[idx:]))
		for _, term := range effortParenTerms {
			if strings.HasPrefix(paren, term) {
				return strings.TrimSpace(display[:idx])
			}
		}
	}
	return display
}

func ensureDefault(choices []domain.OptionChoice) {
	for _, choice := range choices {
		if choice.Default {
			return
		}
	}
	choices[0].Default = true
}

func defaultChoice(choices []domain.OptionChoice) string {
	for _, choice := range choices {
		if choice.Default {
			return choice.ID
		}
	}
	return ""
}

const accountModelsTTL = 10 * time.Minute

type cachedAccountModels struct {
	Models    []domain.Model `json:"models"`
	FetchedAt int64          `json:"fetchedAt,omitempty"`
}

var (
	accountModelsMu       sync.Mutex
	accountModelsCache    = make(map[string]cachedAccountModels)
	accountModelsFilePath string
)

// InitAccountModelsCache sets the file path to persist account models across app runs.
// Supports both the timestamped format and the legacy map[string][]domain.Model format
// (legacy entries load with FetchedAt=0 so they refresh automatically on next fetch).
func InitAccountModelsCache(path string) {
	accountModelsMu.Lock()
	defer accountModelsMu.Unlock()
	accountModelsFilePath = path
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || raw == nil {
		return
	}
	loaded := make(map[string]cachedAccountModels, len(raw))
	for k, v := range raw {
		var entry cachedAccountModels
		if json.Unmarshal(v, &entry) == nil && len(entry.Models) > 0 {
			loaded[k] = entry
			continue
		}
		var legacy []domain.Model
		if json.Unmarshal(v, &legacy) == nil && len(legacy) > 0 {
			loaded[k] = cachedAccountModels{Models: legacy, FetchedAt: 0}
		}
	}
	if len(loaded) > 0 {
		accountModelsCache = loaded
	}
}

func saveAccountModelsLocked() {
	if accountModelsFilePath == "" {
		return
	}
	if data, err := json.Marshal(accountModelsCache); err == nil {
		_ = os.WriteFile(accountModelsFilePath, data, 0o644)
	}
}

// FetchAccountModels retrieves available models for a specific Antigravity account,
// refreshing from the CLI when the cached entry is missing or older than accountModelsTTL.
func FetchAccountModels(ctx context.Context, binary string, accountID, authMode, apiKey string) ([]domain.Model, error) {
	return FetchAccountModelsFresh(ctx, binary, accountID, authMode, apiKey, false)
}

// FetchAccountModelsFresh retrieves available models for a specific Antigravity account.
// When force is true, any cached entry is bypassed and `agy models` is queried live.
func FetchAccountModelsFresh(ctx context.Context, binary string, accountID, authMode, apiKey string, force bool) ([]domain.Model, error) {
	normID := domain.NormalizeAccount(accountID)
	cacheKey := normID
	if authMode == "api_key" {
		cacheKey = "api:" + normID
	}

	var stale []domain.Model
	accountModelsMu.Lock()
	if cached, ok := accountModelsCache[cacheKey]; ok && len(cached.Models) > 0 {
		stale = cached.Models
		if !force && cached.FetchedAt > 0 && time.Since(time.UnixMilli(cached.FetchedAt)) < accountModelsTTL {
			accountModelsMu.Unlock()
			return cached.Models, nil
		}
	}
	accountModelsMu.Unlock()

	var stdout string
	var runErr error
	if authMode == "api_key" && apiKey != "" {
		env := provider.Without(
			shell.BaseEnvironment(),
			"GEMINI_API_KEY",
			"GOOGLE_API_KEY",
			"GOOGLE_GENERATIVE_AI_API_KEY",
			"ANTIGRAVITY_API_KEY",
		)
		env["GEMINI_API_KEY"] = apiKey
		res := provider.RunCommand(ctx, binary, []string{"models"}, env, "")
		runErr = res.Err
		stdout = res.Stdout
	} else {
		// Keyring mode
		SpawnMutex.Lock()
		defer SpawnMutex.Unlock()

		// Activate requested profile (backing up default if needed), then restore default after running.
		if err := DefaultKeyringStore.ActivateProfile(normID); err != nil && normID != domain.DefaultAccountID {
			if len(stale) > 0 {
				return stale, nil
			}
			return nil, err
		}
		if normID != domain.DefaultAccountID {
			defer func() { _ = DefaultKeyringStore.RestoreDefault() }()
		}

		res := provider.RunCommand(ctx, binary, []string{"models"}, nil, "")
		runErr = res.Err
		stdout = res.Stdout
	}

	if runErr != nil {
		if len(stale) > 0 {
			return stale, nil
		}
		return nil, runErr
	}

	models := ParseModels(stdout)
	if len(models) > 0 {
		accountModelsMu.Lock()
		accountModelsCache[cacheKey] = cachedAccountModels{
			Models:    models,
			FetchedAt: time.Now().UnixMilli(),
		}
		saveAccountModelsLocked()
		accountModelsMu.Unlock()
		return models, nil
	}
	if len(stale) > 0 {
		return stale, nil
	}
	return models, nil
}

// InvalidateAccountModels clears the model cache for an account (or all if accountID is empty).
func InvalidateAccountModels(accountID string) {
	accountModelsMu.Lock()
	defer accountModelsMu.Unlock()
	if accountID == "" {
		accountModelsCache = make(map[string]cachedAccountModels)
	} else {
		delete(accountModelsCache, domain.NormalizeAccount(accountID))
		delete(accountModelsCache, "api:"+domain.NormalizeAccount(accountID))
	}
	saveAccountModelsLocked()
}
