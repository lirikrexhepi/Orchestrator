package domain

// ChangeStatus is what happened to a file, normalised across git's index and
// working-tree columns so the UI does not have to read porcelain codes.
type ChangeStatus string

const (
	ChangeAdded      ChangeStatus = "added"
	ChangeModified   ChangeStatus = "modified"
	ChangeDeleted    ChangeStatus = "deleted"
	ChangeRenamed    ChangeStatus = "renamed"
	ChangeCopied     ChangeStatus = "copied"
	ChangeUntracked  ChangeStatus = "untracked"
	ChangeConflicted ChangeStatus = "conflicted"
)

// FileChange is one entry in a changed-files list.
type FileChange struct {
	Path    string       `json:"path"`
	OldPath string       `json:"oldPath,omitempty"`
	Status  ChangeStatus `json:"status"`
	// Staged reports which side of the index the change sits on. A file edited
	// after being staged appears once for each, which is what git itself shows.
	Staged     bool `json:"staged"`
	Insertions int  `json:"insertions"`
	Deletions  int  `json:"deletions"`
	Binary     bool `json:"binary"`
}

// DiffLineKind distinguishes the three row types in a unified diff.
type DiffLineKind string

const (
	DiffContext DiffLineKind = "context"
	DiffAdded   DiffLineKind = "added"
	DiffRemoved DiffLineKind = "removed"
)

// DiffLine is one rendered row. The line numbers are zero when the row does not
// exist on that side, which is what lets the UI leave the gutter blank.
type DiffLine struct {
	Kind    DiffLineKind `json:"kind"`
	Old     int          `json:"old,omitempty"`
	New     int          `json:"new,omitempty"`
	Content string       `json:"content"`
}

// DiffHunk is a contiguous run of changed lines with its surrounding context.
type DiffHunk struct {
	Header string     `json:"header"`
	Lines  []DiffLine `json:"lines"`
}

// DiffFile is everything needed to render one file's changes.
type DiffFile struct {
	Path       string     `json:"path"`
	OldPath    string     `json:"oldPath,omitempty"`
	Hunks      []DiffHunk `json:"hunks"`
	Insertions int        `json:"insertions"`
	Deletions  int        `json:"deletions"`
	Binary     bool       `json:"binary"`
	// Truncated reports that the diff was too large to send in full, so the UI
	// can say so rather than implying the file stops where the text does.
	Truncated bool `json:"truncated"`
}

// Commit is one entry in a branch's history.
type Commit struct {
	SHA     string `json:"sha"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	At      int64  `json:"at"`
	// OnBase marks commits reachable from the base branch, so the UI can
	// separate what an agent added from what it inherited.
	OnBase bool `json:"onBase"`
}

// WorktreeChanges is the whole picture for one checkout: the main repository or
// one agent's isolated worktree.
type WorktreeChanges struct {
	// ThreadID ties the lane to a live agent; empty for the main repository and
	// for worktrees whose agent is no longer running.
	ThreadID string `json:"threadId,omitempty"`
	Title    string `json:"title"`
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Base     string `json:"base,omitempty"`
	IsMain   bool   `json:"isMain"`
	// Orphaned marks a worktree on disk that no task in this session claims —
	// the leftover case worth reviewing before it is removed.
	Orphaned    bool         `json:"orphaned"`
	Files       []FileChange `json:"files"`
	Commits     []Commit     `json:"commits"`
	Ahead       int          `json:"ahead"`
	Upstream    string       `json:"upstream,omitempty"`
	Unpushed    int          `json:"unpushed"`
	Unpulled    int          `json:"unpulled"`
	BranchBase  string       `json:"branchBase,omitempty"`
	BranchFiles []FileStat   `json:"branchFiles"`
	Error       string       `json:"error,omitempty"`
}

type FileStat struct {
	Path       string `json:"path"`
	Insertions int    `json:"insertions"`
	Deletions  int    `json:"deletions"`
	Binary     bool   `json:"binary"`
}

type BranchInfo struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
	Remote  bool   `json:"remote"`
}
