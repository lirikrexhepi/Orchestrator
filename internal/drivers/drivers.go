package drivers

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"composer/internal/antigravity"
	"composer/internal/claude"
	"composer/internal/cliupdate"
	"composer/internal/codex"
	"composer/internal/domain"
	"composer/internal/opencode"
	"composer/internal/provider"
)

// All returns the built-in driver set. Registering a new agent CLI means adding
// one entry here plus its adapter package.
func All() []provider.Driver {
	return []provider.Driver{
		&claudeDriver{},
		&codexDriver{},
		&antigravityDriver{},
		&openCodeDriver{},
	}
}

func binaryFor(settings domain.ProviderSettings, fallback string) string {
	if settings.BinaryPath != "" {
		return settings.BinaryPath
	}
	return fallback
}

type claudeDriver struct{}

func (d *claudeDriver) Kind() domain.DriverKind { return domain.DriverClaude }
func (d *claudeDriver) DisplayName() string     { return "Claude Code" }

func (d *claudeDriver) DefaultSettings() domain.ProviderSettings {
	return domain.ProviderSettings{Enabled: false}
}

// Probe gates the static catalog on the installed CLI version, since the Claude
// CLI cannot enumerate its own models. It also carries the update advisory so
// the UI can offer a newer release without its own version plumbing.
func (d *claudeDriver) Probe(ctx context.Context, settings domain.ProviderSettings) domain.ProviderSnapshot {
	snapshot := provider.ProbeVersion(ctx, binaryFor(settings, "claude"), settings)
	if snapshot.Availability == domain.AvailabilityReady {
		snapshot.Models = claude.Models(snapshot.Version)
		if advisory := cliupdate.Check(ctx, cliupdate.ClaudePackage, snapshot.Version); advisory.Latest != "" {
			snapshot.Update = &domain.ProviderUpdate{
				Installed: advisory.Installed,
				Latest:    advisory.Latest,
				Available: advisory.Available,
				CheckedAt: advisory.CheckedAt,
			}
		}
	}
	return snapshot
}

func (d *claudeDriver) NewAdapter(settings domain.ProviderSettings, emit provider.Emitter) (provider.Adapter, error) {
	return claude.NewAdapter(settings, emit), nil
}

type antigravityDriver struct{}

func (d *antigravityDriver) Kind() domain.DriverKind { return domain.DriverAntigravity }
func (d *antigravityDriver) DisplayName() string     { return "Antigravity" }

func (d *antigravityDriver) DefaultSettings() domain.ProviderSettings {
	return domain.ProviderSettings{Enabled: false}
}

func (d *antigravityDriver) Probe(ctx context.Context, settings domain.ProviderSettings) domain.ProviderSnapshot {
	binary := binaryFor(settings, "agy")
	snapshot := provider.ProbeVersion(ctx, binary, settings)
	if snapshot.Availability != domain.AvailabilityReady {
		return snapshot
	}
	if result := provider.RunCommand(ctx, binary, []string{"models"}, nil, ""); result.Err == nil {
		snapshot.Models = antigravity.ParseModels(result.Stdout)
	}
	return snapshot
}

func (d *antigravityDriver) NewAdapter(settings domain.ProviderSettings, emit provider.Emitter) (provider.Adapter, error) {
	return antigravity.NewAdapter(settings, emit), nil
}

type codexDriver struct{}

func (d *codexDriver) Kind() domain.DriverKind { return domain.DriverCodex }
func (d *codexDriver) DisplayName() string     { return "Codex" }

func (d *codexDriver) DefaultSettings() domain.ProviderSettings {
	return domain.ProviderSettings{Enabled: false}
}

func (d *codexDriver) Probe(ctx context.Context, settings domain.ProviderSettings) domain.ProviderSnapshot {
	snapshot := provider.ProbeVersion(ctx, binaryFor(settings, "codex"), settings)
	if snapshot.Availability == domain.AvailabilityReady {
		snapshot.Models = codex.Models()
	}
	return snapshot
}

func (d *codexDriver) NewAdapter(settings domain.ProviderSettings, emit provider.Emitter) (provider.Adapter, error) {
	return codex.NewAdapter(settings, emit), nil
}

type openCodeDriver struct{}

func (d *openCodeDriver) Kind() domain.DriverKind { return domain.DriverOpenCode }
func (d *openCodeDriver) DisplayName() string     { return "OpenCode" }

func (d *openCodeDriver) DefaultSettings() domain.ProviderSettings {
	return domain.ProviderSettings{Enabled: false}
}

func (d *openCodeDriver) Probe(ctx context.Context, settings domain.ProviderSettings) domain.ProviderSnapshot {
	// An external server needs no local binary, so availability is decided by
	// the configured URL rather than a version probe.
	if strings.TrimSpace(settings.ServerURL) != "" {
		return domain.ProviderSnapshot{
			Availability: domain.AvailabilityReady,
			Message:      "Using external server at " + settings.ServerURL,
		}
	}

	snapshot := provider.ProbeVersion(ctx, binaryFor(settings, "opencode"), settings)
	if snapshot.Availability != domain.AvailabilityReady {
		return snapshot
	}
	if opencode.MajorOf(snapshot.Version) >= 2 {
		snapshot.Models = d.modelsV2(ctx, settings)
	} else {
		snapshot.Models = d.models(ctx, settings)
	}
	return snapshot
}

func (d *openCodeDriver) modelsV2(ctx context.Context, settings domain.ProviderSettings) []domain.Model {
	infos, err := opencode.ListModels(ctx, settings)
	if err != nil {
		return nil
	}
	models := make([]domain.Model, 0, len(infos))
	for _, info := range infos {
		name := info.Name
		if name == "" {
			name = formatOpenCodeModelName(info.ID)
		}
		model := domain.Model{ID: info.ID, DisplayName: name}
		variants := make(map[string]openCodeVariant, len(info.Variants))
		for _, variant := range info.Variants {
			variants[variant] = openCodeVariant{}
		}
		if option, ok := variantOption(variants); ok {
			model.Options = []domain.OptionDescriptor{option}
		}
		models = append(models, model)
	}
	return models
}

func (d *openCodeDriver) models(ctx context.Context, settings domain.ProviderSettings) []domain.Model {
	binary := binaryFor(settings, "opencode")
	// --verbose prints each model's JSON after its id, which carries the
	// variants (reasoning effort levels) the prompt API accepts.
	if result := provider.RunCommand(ctx, binary, []string{"models", "--verbose"}, nil, ""); result.Err == nil {
		if models := parseOpenCodeModels(result.Stdout); len(models) > 0 {
			return models
		}
	}
	result := provider.RunCommand(ctx, binary, []string{"models"}, nil, "")
	if result.Err != nil {
		return nil
	}
	return parseOpenCodeModels(result.Stdout)
}

var openCodeModelLine = regexp.MustCompile(`^[^\s{}\[\]"]+/[^\s]+$`)

// parseOpenCodeModels reads `opencode models [--verbose]`: one providerID/
// modelID per line, each optionally followed by that model's JSON.
func parseOpenCodeModels(output string) []domain.Model {
	var models []domain.Model
	var id string
	var body strings.Builder
	flush := func() {
		if id == "" {
			return
		}
		model := domain.Model{ID: id, DisplayName: formatOpenCodeModelName(id)}
		if raw := strings.TrimSpace(body.String()); raw != "" {
			var meta struct {
				Variants map[string]openCodeVariant `json:"variants"`
			}
			if json.Unmarshal([]byte(raw), &meta) == nil {
				if option, ok := variantOption(meta.Variants); ok {
					model.Options = []domain.OptionDescriptor{option}
				}
			}
		}
		models = append(models, model)
		id = ""
		body.Reset()
	}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line == trimmed && openCodeModelLine.MatchString(trimmed) {
			flush()
			id = trimmed
			continue
		}
		if id != "" {
			body.WriteString(line)
			body.WriteString("\n")
		}
	}
	flush()
	return models
}

// variantOrder ranks common effort names; unknown ones sort after, by name.
var variantOrder = map[string]int{"none": 0, "minimal": 1, "low": 2, "medium": 3, "high": 4, "xhigh": 5, "max": 6}

type openCodeVariant struct {
	Disabled bool `json:"disabled"`
}

func variantOption(variants map[string]openCodeVariant) (domain.OptionDescriptor, bool) {
	names := make([]string, 0, len(variants))
	for name, variant := range variants {
		if variant.Disabled {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return domain.OptionDescriptor{}, false
	}
	sort.Slice(names, func(i, j int) bool {
		ri, iok := variantOrder[names[i]]
		rj, jok := variantOrder[names[j]]
		if iok && jok {
			return ri < rj
		}
		if iok != jok {
			return iok
		}
		return names[i] < names[j]
	})
	choices := make([]domain.OptionChoice, 0, len(names)+1)
	// "Default" sends no variant, leaving the model's own default in effect.
	choices = append(choices, domain.OptionChoice{ID: "", Label: "Default", Default: true})
	for _, name := range names {
		choices = append(choices, domain.OptionChoice{ID: name, Label: strings.ToUpper(name[:1]) + name[1:]})
	}
	return domain.OptionDescriptor{
		ID: domain.OptionEffort, Label: "Effort", Type: domain.OptionSelect, Choices: choices, Default: "",
	}, true
}

func formatOpenCodeModelName(id string) string {
	name := id
	if strings.HasPrefix(strings.ToLower(name), "opencode/") {
		name = name[9:]
	} else if strings.HasPrefix(strings.ToLower(name), "opencode:") {
		name = name[9:]
	}
	parts := strings.Fields(strings.ReplaceAll(name, "-", " "))
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	displayName := strings.Join(parts, " ")
	if displayName == "" {
		return id
	}
	return displayName
}

func (d *openCodeDriver) NewAdapter(settings domain.ProviderSettings, emit provider.Emitter) (provider.Adapter, error) {
	return opencode.NewRouter(settings, emit), nil
}
