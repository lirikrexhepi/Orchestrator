package claudeimport

import (
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	pageIndexTTL    = 30 * time.Second
	pageTimeBudget  = 350 * time.Millisecond
	pageMaxLimit    = 50
	pageDefaultSize = 5
)

type Page struct {
	Sessions []ExternalSession `json:"sessions"`
	Next     int               `json:"next"`
}

type transcriptFile struct {
	path    string
	modTime time.Time
}

var (
	pageMu      sync.Mutex
	pageFiles   []transcriptFile
	pageBuiltAt time.Time
)

func transcriptIndex(refresh bool) ([]transcriptFile, error) {
	pageMu.Lock()
	defer pageMu.Unlock()
	if !refresh && pageFiles != nil && time.Since(pageBuiltAt) < pageIndexTTL {
		return pageFiles, nil
	}
	root := ProjectsDir()
	files := []transcriptFile{}
	if root != "" {
		walkTranscripts(root, func(path string, modTime time.Time) {
			files = append(files, transcriptFile{path: path, modTime: modTime})
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
	if len(files) > maxListFiles {
		files = files[:maxListFiles]
	}
	pageFiles = files
	pageBuiltAt = time.Now()
	return files, nil
}

func walkTranscripts(root string, visit func(path string, modTime time.Time)) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		full := root + string(os.PathSeparator) + entry.Name()
		if entry.IsDir() {
			walkTranscripts(full, visit)
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		visit(full, info.ModTime())
	}
}

func ListPage(offset, limit int) (Page, error) {
	offset, limit = clampPage(offset, limit)
	files, err := transcriptIndex(offset == 0)
	if err != nil {
		return Page{Sessions: []ExternalSession{}, Next: -1}, err
	}
	out := make([]ExternalSession, 0, limit)
	started := time.Now()
	i := offset
	for ; i < len(files) && len(out) < limit; i++ {
		if i > offset && time.Since(started) > pageTimeBudget {
			break
		}
		session := describe(files[i].path)
		if session == nil || session.AgentRun {
			continue
		}
		out = append(out, *session)
	}
	next := i
	if i >= len(files) {
		next = -1
	}
	return Page{Sessions: out, Next: next}, nil
}
