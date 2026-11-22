package domain

import (
	"path/filepath"
	"strings"
)

type DriverKind string

const (
	DriverClaude      DriverKind = "claude"
	DriverCodex       DriverKind = "codex"
	DriverAntigravity DriverKind = "antigravity"
	DriverOpenCode    DriverKind = "opencode"
)

// DriverLabel is the human-facing provider name used in switch dividers.
func DriverLabel(d DriverKind) string {
	switch d {
	case DriverAntigravity:
		return "Antigravity"
	case DriverOpenCode:
		return "OpenCode"
	case DriverClaude:
		return "Claude Code"
	case DriverCodex:
		return "Codex"
	default:
		return string(d)
	}
}

type Transport string

const (
	TransportACP        Transport = "acp"
	TransportStreamJSON Transport = "stream-json"
	TransportAppServer  Transport = "app-server"
	TransportHTTP       Transport = "http"
)

type Availability string

const (
	AvailabilityPending     Availability = "pending"
	AvailabilityReady       Availability = "ready"
	AvailabilityUnavailable Availability = "unavailable"
	AvailabilityUnauthed    Availability = "unauthenticated"
)

type AccountModelDefault struct {
	Model   string       `json:"model,omitempty"`
	Options ModelOptions `json:"options,omitempty"`
}

type ProviderSettings struct {
	BinaryPath      string                         `json:"binaryPath,omitempty"`
	LaunchArgs      string                         `json:"launchArgs,omitempty"`
	Env             map[string]string              `json:"env,omitempty"`
	ServerURL       string                         `json:"serverUrl,omitempty"`
	APIEndpoint     string                         `json:"apiEndpoint,omitempty"`
	Model           string                         `json:"model,omitempty"`
	Options         ModelOptions                   `json:"options,omitempty"`
	AccountDefaults map[string]AccountModelDefault `json:"accountDefaults,omitempty"`
	PrintTimeout    string                         `json:"printTimeout,omitempty"`
	Enabled         bool                           `json:"enabled"`
	Unset           []string                       `json:"-"`
}

type ProviderSnapshot struct {
	InstanceID   string       `json:"instanceId"`
	Driver       DriverKind   `json:"driver"`
	DisplayName  string       `json:"displayName"`
	Availability Availability `json:"availability"`
	Version      string       `json:"version,omitempty"`
	CommandPath  string       `json:"commandPath,omitempty"`
	Message      string       `json:"message,omitempty"`
	Models       []Model      `json:"models,omitempty"`
	CheckedAt    int64        `json:"checkedAt"`
	// Settings are the user's stored choices for this CLI, carried on the
	// snapshot so the UI can honour a preferred model without a second call per
	// provider on every refresh.
	Settings ProviderSettings `json:"settings"`
	Accounts []Account        `json:"accounts,omitempty"`
	// Update names a newer published release of the backing CLI, when the
	// last check found one. Absent while the check has not run or failed.
	Update *ProviderUpdate `json:"update,omitempty"`
}

// ProviderUpdate is the version advisory for one CLI: what is installed,
// what is published, and whether they differ.
type ProviderUpdate struct {
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	CheckedAt int64  `json:"checkedAt"`
}

const DefaultAccountID = "default"

type Account struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	ConfigDir string  `json:"configDir,omitempty"`
	AuthMode  string  `json:"authMode,omitempty"`
	Models    []Model `json:"models,omitempty"`
}

func (a Account) IsDefault() bool {
	return a.ID == "" || a.ID == DefaultAccountID
}

func NormalizeAccount(id string) string {
	if id == "" {
		return DefaultAccountID
	}
	return id
}

type Model struct {
	ID          string             `json:"id"`
	DisplayName string             `json:"displayName"`
	Default     bool               `json:"default,omitempty"`
	Options     []OptionDescriptor `json:"options,omitempty"`
}

type OptionType string

const (
	OptionSelect  OptionType = "select"
	OptionBoolean OptionType = "boolean"
)

// OptionDescriptor describes one tunable knob for a model: a select (effort,
// context window) or a boolean (thinking, fast mode). Providers advertise these
// per model so the UI renders whatever a CLI actually supports.
type OptionDescriptor struct {
	ID      string         `json:"id"`
	Label   string         `json:"label"`
	Type    OptionType     `json:"type"`
	Choices []OptionChoice `json:"choices,omitempty"`
	Default any            `json:"default,omitempty"`
}

type OptionChoice struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Default bool   `json:"default,omitempty"`
}

const (
	OptionEffort        = "effort"
	OptionThinking      = "thinking"
	OptionFastMode      = "fastMode"
	OptionContextWindow = "contextWindow"
)

type PermissionMode string

const (
	PermissionDefault     PermissionMode = "default"
	PermissionAcceptEdits PermissionMode = "acceptEdits"
	PermissionPlan        PermissionMode = "plan"
	PermissionBypass      PermissionMode = "bypassPermissions"
)

type MCPConfig struct {
	ServerName string `json:"-"`
	Endpoint   string `json:"-"`
	AuthHeader string `json:"-"`
}

type SessionStartInput struct {
	ThreadID   string         `json:"threadId"`
	InstanceID string         `json:"instanceId"`
	Account    string         `json:"account,omitempty"`
	Cwd        string         `json:"cwd"`
	Model      string         `json:"model,omitempty"`
	Permission PermissionMode `json:"permissionMode,omitempty"`
	Resume     string         `json:"resume,omitempty"`
	Fork       bool           `json:"fork,omitempty"`
	Options    ModelOptions   `json:"options,omitempty"`
	// PlanOnly denies the session every tool. The orchestrator delegates rather
	// than works, and without this it explores the repo instead of answering.
	PlanOnly   bool         `json:"planOnly,omitempty"`
	Skills     *SkillPolicy `json:"skills,omitempty"`
	MCP        *MCPConfig   `json:"-"`
	IsSubagent bool         `json:"isSubagent,omitempty"`
}

type SkillMode string

const (
	SkillsOnly SkillMode = "only"
	SkillsNone SkillMode = "none"
)

type SkillRef struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type SkillPolicy struct {
	Mode   SkillMode  `json:"mode"`
	Skills []SkillRef `json:"skills,omitempty"`
	Denied []string   `json:"denied,omitempty"`
}

func (p *SkillPolicy) Instruction() string {
	if p == nil || p.Mode != SkillsOnly || len(p.Skills) == 0 {
		return ""
	}
	named := make([]string, 0, len(p.Skills))
	for _, skill := range p.Skills {
		if skill.Path == "" {
			named = append(named, skill.Name)
			continue
		}
		named = append(named, skill.Name+" ("+filepath.Join(skill.Path, "SKILL.md")+")")
	}
	return "Before you start, load each of these skills and follow them throughout the task, reading the SKILL.md at the given path if your skill tool does not list it: " + strings.Join(named, ", ") + "."
}

// ModelOptions carries the user's selections for a model's OptionDescriptors,
// keyed by descriptor id.
type ModelOptions map[string]any

func (o ModelOptions) String(id string) string {
	if value, ok := o[id].(string); ok {
		return value
	}
	return ""
}

func (o ModelOptions) Bool(id string) bool {
	value, _ := o[id].(bool)
	return value
}

type Session struct {
	ThreadID          string     `json:"threadId"`
	InstanceID        string     `json:"instanceId"`
	Driver            DriverKind `json:"driver"`
	Account           string     `json:"account,omitempty"`
	ProviderSessionID string     `json:"providerSessionId,omitempty"`
	Cwd               string     `json:"cwd"`
	Model             string     `json:"model,omitempty"`
	StartedAt         int64      `json:"startedAt"`
}

type SendTurnInput struct {
	ThreadID string    `json:"threadId"`
	TurnID   string    `json:"turnId"`
	Text     string    `json:"text"`
	Files    []FileRef `json:"files,omitempty"`
}

type FileRef struct {
	Path string `json:"path"`
	MIME string `json:"mime,omitempty"`
}

type ApprovalDecision string

const (
	ApprovalAllowOnce   ApprovalDecision = "allowOnce"
	ApprovalAllowAlways ApprovalDecision = "allowAlways"
	ApprovalDeny        ApprovalDecision = "deny"
	ApprovalCancel      ApprovalDecision = "cancel"
)

// NormalizeDecision maps the option ids UIs send ("once", "always",
// "reject") onto canonical decisions, so a Deny button can never be read as
// an approval by an adapter that only recognises the canonical names.
func NormalizeDecision(raw ApprovalDecision) ApprovalDecision {
	switch raw {
	case "once", "allow", "approve", "approved", ApprovalAllowOnce:
		return ApprovalAllowOnce
	case "always", "allow_always", ApprovalAllowAlways:
		return ApprovalAllowAlways
	case "reject", "denied", ApprovalDeny:
		return ApprovalDeny
	case "abort", ApprovalCancel:
		return ApprovalCancel
	default:
		return ApprovalDeny
	}
}
