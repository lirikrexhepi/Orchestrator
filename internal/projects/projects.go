package projects

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const storeFile = "projects.json"

// Project is a directory the user works in. Composer spawns every agent with
// this as its working directory, which is what stops each request having to
// name the path again.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	// IsGit is recorded at add time so the picker can label entries without
	// shelling out to git for every row on every open.
	IsGit   bool  `json:"isGit"`
	AddedAt int64 `json:"addedAt"`
	UsedAt  int64 `json:"usedAt,omitempty"`
	// Order is the selection counter this entry was last touched at. UsedAt is
	// shown to the user; this is what actually sorts, so two selections in the
	// same millisecond still order correctly.
	Order    int64             `json:"order,omitempty"`
	Missing  bool              `json:"missing,omitempty"`
	Accounts map[string]string `json:"accounts,omitempty"`
}

// state is the on-disk shape: the list plus which entry is active.
//
// Seq orders selections. A wall clock cannot: adding and activating within the
// same millisecond produce equal timestamps, which loses the ordering the
// picker depends on.
type state struct {
	Projects []Project `json:"projects"`
	ActiveID string    `json:"activeId,omitempty"`
	Seq      int64     `json:"seq,omitempty"`
}

var (
	ErrNotFound      = errors.New("project not found")
	ErrNotDir        = errors.New("not a directory")
	ErrAlreadyExists = errors.New("another project already uses this folder")
)

// Store keeps the project list between runs.
//
// A store that cannot read its file starts empty rather than failing: a lost
// project list is an inconvenience, never a reason the app will not open.
type Store struct {
	path string

	mu    sync.Mutex
	saved state
}

func New(dir string) *Store {
	store := &Store{path: filepath.Join(dir, storeFile)}

	payload, err := os.ReadFile(store.path)
	if err != nil {
		return store
	}
	var stored state
	if json.Unmarshal(payload, &stored) == nil {
		store.saved = stored
		store.backfillOrder()
	}
	return store
}

// backfillOrder gives entries written before ordering existed a sensible
// position, falling back to the timestamps those versions did record. Without
// it every old entry sorts equal at zero and the picker order looks random.
func (s *Store) backfillOrder() {
	ordered := make([]*Project, 0, len(s.saved.Projects))
	for i := range s.saved.Projects {
		if s.saved.Projects[i].Order > s.saved.Seq {
			s.saved.Seq = s.saved.Projects[i].Order
		}
		if s.saved.Projects[i].Order == 0 {
			ordered = append(ordered, &s.saved.Projects[i])
		}
	}
	if len(ordered) == 0 {
		return
	}

	sort.SliceStable(ordered, func(a, b int) bool {
		left, right := ordered[a], ordered[b]
		if left.UsedAt != right.UsedAt {
			return left.UsedAt < right.UsedAt
		}
		return left.AddedAt < right.AddedAt
	})
	for _, project := range ordered {
		s.saved.Seq++
		project.Order = s.saved.Seq
	}
}

// List reports every project: newly added first, then the user's own
// arrangement. Selecting a project never reorders the list.
//
// Existence is re-checked on every list rather than trusted from the file: a
// directory can be moved or deleted between runs, and silently spawning an
// agent into a path that is gone fails in a way that looks like a CLI bug.
func (s *Store) List() []Project {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Project, len(s.saved.Projects))
	copy(out, s.saved.Projects)
	for i := range out {
		out[i].Missing = !isDir(out[i].Path)
	}

	sort.SliceStable(out, func(a, b int) bool {
		return out[a].Order > out[b].Order
	})
	return out
}

// Active returns the selected project, if one is still present.
func (s *Store) Active() (Project, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, project := range s.saved.Projects {
		if project.ID == s.saved.ActiveID {
			project.Missing = !isDir(project.Path)
			return project, true
		}
	}
	return Project{}, false
}

// ActivePath is the working directory agents should start in. An empty string
// means no project is chosen, which callers treat as the app's own directory.
func (s *Store) ActivePath() string {
	project, ok := s.Active()
	if !ok || project.Missing {
		return ""
	}
	return project.Path
}

// Add records a directory, returning the existing entry when it is already
// known so adding the same folder twice selects it rather than duplicating it.
func (s *Store) Add(path string, isGit bool) (Project, error) {
	return s.add(path, isGit, true)
}

// Remember records a directory without selecting it. The phone adds projects
// this way so it never switches the project the desktop is working in.
func (s *Store) Remember(path string, isGit bool) (Project, error) {
	return s.add(path, isGit, false)
}

func (s *Store) add(path string, isGit bool, activate bool) (Project, error) {
	clean, err := normalize(path)
	if err != nil {
		return Project{}, err
	}

	s.mu.Lock()
	for i, existing := range s.saved.Projects {
		if !samePath(existing.Path, clean) {
			continue
		}
		// Re-adding a known folder selects it, and counts as using it, so it
		// rises to the top of the picker like any other selection.
		s.saved.Seq++
		s.saved.Projects[i].UsedAt = time.Now().UnixMilli()
		s.saved.Projects[i].Order = s.saved.Seq
		if activate {
			s.saved.ActiveID = existing.ID
		}
		updated := s.saved.Projects[i]
		s.mu.Unlock()
		if err := s.persist(); err != nil {
			return Project{}, err
		}
		return updated, nil
	}

	now := time.Now().UnixMilli()
	s.saved.Seq++
	project := Project{
		ID:      newID(clean, s.saved.Seq),
		Name:    filepath.Base(clean),
		Path:    clean,
		IsGit:   isGit,
		AddedAt: now,
		UsedAt:  now,
		Order:   s.saved.Seq,
	}
	s.saved.Projects = append(s.saved.Projects, project)
	if activate {
		s.saved.ActiveID = project.ID
	}
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return Project{}, err
	}
	return project, nil
}

// Remove forgets a project. Removing the active one clears the selection rather
// than silently promoting another, so the next spawn cannot land somewhere the
// user did not choose.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	kept := s.saved.Projects[:0]
	found := false
	for _, project := range s.saved.Projects {
		if project.ID == id {
			found = true
			continue
		}
		kept = append(kept, project)
	}
	s.saved.Projects = kept
	if s.saved.ActiveID == id {
		s.saved.ActiveID = ""
	}
	s.mu.Unlock()

	if !found {
		return ErrNotFound
	}
	return s.persist()
}

// ByID returns a single saved project by its ID with live missing status.
func (s *Store) ByID(id string) (Project, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, project := range s.saved.Projects {
		if project.ID == id {
			project.Missing = !isDir(project.Path)
			return project, true
		}
	}
	return Project{}, false
}

// Relocate updates an existing project's folder path in place, preserving its
// ID, picker order, active status, and provider account bindings. Returns the
// previous path alongside the updated project so callers can migrate history
// and project memory.
func (s *Store) Relocate(id, newPath string, isGit bool) (string, Project, error) {
	clean, err := normalize(newPath)
	if err != nil {
		return "", Project{}, err
	}

	s.mu.Lock()
	index := -1
	for i := range s.saved.Projects {
		if s.saved.Projects[i].ID == id {
			index = i
			continue
		}
		if samePath(s.saved.Projects[i].Path, clean) {
			s.mu.Unlock()
			return "", Project{}, ErrAlreadyExists
		}
	}
	if index < 0 {
		s.mu.Unlock()
		return "", Project{}, ErrNotFound
	}

	oldPath := s.saved.Projects[index].Path
	s.saved.Projects[index].Path = clean
	s.saved.Projects[index].Name = filepath.Base(clean)
	s.saved.Projects[index].IsGit = isGit
	s.saved.Projects[index].Missing = false
	updated := s.saved.Projects[index]
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return "", Project{}, err
	}
	return oldPath, updated, nil
}

// Activate selects a project and stamps its last-used time. It deliberately
// leaves the picker order alone: the order is the user's own arrangement,
// with newly added projects arriving at the top.
func (s *Store) Activate(id string) (Project, error) {
	s.mu.Lock()
	var selected Project
	found := false
	for i := range s.saved.Projects {
		if s.saved.Projects[i].ID != id {
			continue
		}
		s.saved.Projects[i].UsedAt = time.Now().UnixMilli()
		selected = s.saved.Projects[i]
		found = true
		break
	}
	if found {
		s.saved.ActiveID = id
	}
	s.mu.Unlock()

	if !found {
		return Project{}, ErrNotFound
	}
	if err := s.persist(); err != nil {
		return Project{}, err
	}
	selected.Missing = !isDir(selected.Path)
	return selected, nil
}

// Move places a project at an explicit position, which List then keeps.
// Out-of-range indexes clamp to the ends; the active selection is untouched,
// so rearranging never switches the project agents start in.
func (s *Store) Move(id string, toIndex int) error {
	s.mu.Lock()
	// Arrange in the order List reports, so the index matches the position
	// the user sees and drags to.
	ordered := make([]Project, len(s.saved.Projects))
	copy(ordered, s.saved.Projects)
	sort.SliceStable(ordered, func(a, b int) bool { return ordered[a].Order > ordered[b].Order })

	from := -1
	for i, project := range ordered {
		if project.ID == id {
			from = i
			break
		}
	}
	if from < 0 {
		s.mu.Unlock()
		return ErrNotFound
	}

	moved := ordered[from]
	ordered = append(ordered[:from], ordered[from+1:]...)
	if toIndex < 0 {
		toIndex = 0
	}
	if toIndex > len(ordered) {
		toIndex = len(ordered)
	}
	ordered = append(ordered[:toIndex], append([]Project{moved}, ordered[toIndex:]...)...)
	// Resequence below the selection counter so projects added later still
	// arrive at the top, ahead of any manual arrangement.
	for i := range ordered {
		ordered[i].Order = int64(len(ordered) - i)
	}
	s.saved.Projects = ordered
	s.mu.Unlock()

	return s.persist()
}

func (s *Store) SetAccount(id, driver, account string) (Project, error) {
	s.mu.Lock()
	var updated Project
	found := false
	for i := range s.saved.Projects {
		if s.saved.Projects[i].ID != id {
			continue
		}
		accounts := make(map[string]string, len(s.saved.Projects[i].Accounts)+1)
		for key, value := range s.saved.Projects[i].Accounts {
			accounts[key] = value
		}
		if account == "" {
			delete(accounts, driver)
		} else {
			accounts[driver] = account
		}
		if len(accounts) == 0 {
			accounts = nil
		}
		s.saved.Projects[i].Accounts = accounts
		updated = s.saved.Projects[i]
		found = true
		break
	}
	s.mu.Unlock()

	if !found {
		return Project{}, ErrNotFound
	}
	if err := s.persist(); err != nil {
		return Project{}, err
	}
	updated.Missing = !isDir(updated.Path)
	return updated, nil
}

func (s *Store) AccountFor(dir, driver string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	best, bestLen := "", -1
	for _, project := range s.saved.Projects {
		account := project.Accounts[driver]
		if account == "" || !within(dir, project.Path) {
			continue
		}
		if len(project.Path) > bestLen {
			best, bestLen = account, len(project.Path)
		}
	}
	return best
}

func within(dir, root string) bool {
	if samePath(dir, root) {
		return true
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(dir))
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// persist writes the whole file via a temp file, so a crash mid-write cannot
// leave the list unreadable and drop every project the user added.
func (s *Store) persist() error {
	s.mu.Lock()
	payload, err := json.MarshalIndent(s.saved, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	temp := s.path + ".tmp"
	if err := os.WriteFile(temp, payload, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temp, s.path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

// normalize resolves a chosen path to an absolute directory, rejecting files
// so a mis-picked entry fails here rather than when a CLI cannot chdir into it.
func normalize(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", ErrNotDir
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if !isDir(abs) {
		return "", ErrNotDir
	}
	return abs, nil
}

func isDir(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// samePath compares directories the way the host filesystem does, so Windows
// does not end up with the same folder listed twice under different casing.
func samePath(a, b string) bool {
	if filepath.Separator == '\\' {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// newID derives a readable, unique id from the folder name and the store's
// selection counter. The counter rather than a timestamp is what guarantees
// uniqueness: two folders with the same base name can be added within the same
// millisecond.
func newID(path string, seq int64) string {
	base := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 32
		case r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, filepath.Base(path))

	base = strings.Trim(base, "-")
	if base == "" {
		base = "project"
	}
	return base + "-" + strconv.FormatInt(seq, 36)
}
