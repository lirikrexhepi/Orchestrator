package history

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"composer/internal/domain"
)

func tailEvent(kind domain.EventKind, thread, turn, text string, seq uint64) domain.RuntimeEvent {
	return domain.RuntimeEvent{Kind: kind, ThreadID: thread, TurnID: turn, Seq: seq, At: 1700000000000, Text: text}
}

func appendTail(t *testing.T, store *Store, ws, thread string, events ...domain.RuntimeEvent) {
	t.Helper()
	for _, ev := range events {
		if err := store.Append(ws, thread, ev); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
}

func rawLines(t *testing.T, store *Store, ws, thread string) [][]byte {
	t.Helper()
	if err := store.Flush(ws); err != nil {
		t.Fatalf("flush: %v", err)
	}
	path := filepath.Join(store.root, safeName(ws), safeName(thread)+transcriptExt)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	var lines [][]byte
	for _, line := range bytes.Split(payload, []byte("\n")) {
		trimmed := bytes.Trim(line, "\r\n\t \x00")
		if len(trimmed) > 0 {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

func userBoundaries(lines [][]byte) []int {
	var out []int
	for i, line := range lines {
		if bytes.Contains(line, userMessageLineMarker) {
			out = append(out, i)
		}
	}
	return out
}

func suffixSize(lines [][]byte, from int) int {
	total := 0
	for i := from; i < len(lines); i++ {
		total += len(lines[i]) + 1
	}
	return total
}

func TestLoadTailKeepsSmallTranscriptWhole(t *testing.T) {
	store := New(t.TempDir())
	defer store.Close()
	ws := "ws-tail-small"
	if err := store.SaveMeta(Meta{Workspace: domain.Workspace{ID: ws}}); err != nil {
		t.Fatal(err)
	}
	appendTail(t, store, ws, "thread-a",
		tailEvent(domain.EventUserMessage, "thread-a", "t1", "hello", 1),
		tailEvent(domain.EventAgentMessage, "thread-a", "t1", "hi there", 2),
	)

	full, err := store.Load(ws)
	if err != nil {
		t.Fatal(err)
	}
	tail, err := store.LoadTail(ws, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(tail.Transcripts["thread-a"]), len(full.Transcripts["thread-a"]); got != want {
		t.Fatalf("small transcript was cut: got %d events, want %d", got, want)
	}
	if len(tail.Truncated) != 0 {
		t.Fatalf("expected no truncated threads, got %v", tail.Truncated)
	}
}

func TestLoadTailCutsAtUserBoundary(t *testing.T) {
	store := New(t.TempDir())
	defer store.Close()
	ws := "ws-tail-cut"
	if err := store.SaveMeta(Meta{Workspace: domain.Workspace{ID: ws}}); err != nil {
		t.Fatal(err)
	}
	appendTail(t, store, ws, "thread-a",
		tailEvent(domain.EventUserMessage, "thread-a", "t1", "first", 1),
		tailEvent(domain.EventAgentMessage, "thread-a", "t1", "reply one", 2),
		tailEvent(domain.EventUserMessage, "thread-a", "t2", "second", 3),
		tailEvent(domain.EventAgentMessage, "thread-a", "t2", "reply two", 4),
		tailEvent(domain.EventUserMessage, "thread-a", "t3", "third", 5),
		tailEvent(domain.EventAgentMessage, "thread-a", "t3", "reply three", 6),
	)

	lines := rawLines(t, store, ws, "thread-a")
	boundaries := userBoundaries(lines)
	if len(boundaries) != 3 {
		t.Fatalf("expected 3 user boundaries, got %v", boundaries)
	}

	tail, err := store.LoadTail(ws, suffixSize(lines, boundaries[1]))
	if err != nil {
		t.Fatal(err)
	}
	events := tail.Transcripts["thread-a"]
	if len(events) != 4 {
		t.Fatalf("expected the newest two turns (4 events), got %d: %+v", len(events), events)
	}
	if events[0].Kind != domain.EventUserMessage || events[0].Text != "second" {
		t.Fatalf("tail must start at a user.message boundary, got %+v", events[0])
	}
	if got := tail.Truncated["thread-a"]; got != boundaries[1] {
		t.Fatalf("Truncated = %d, want %d", got, boundaries[1])
	}
}

func TestLoadTailAlwaysKeepsLastTurn(t *testing.T) {
	store := New(t.TempDir())
	defer store.Close()
	ws := "ws-tail-min"
	if err := store.SaveMeta(Meta{Workspace: domain.Workspace{ID: ws}}); err != nil {
		t.Fatal(err)
	}
	appendTail(t, store, ws, "thread-a",
		tailEvent(domain.EventUserMessage, "thread-a", "t1", "first", 1),
		tailEvent(domain.EventAgentMessage, "thread-a", "t1", "reply one", 2),
		tailEvent(domain.EventUserMessage, "thread-a", "t2", "second", 3),
		tailEvent(domain.EventAgentMessage, "thread-a", "t2", "reply two", 4),
	)

	tail, err := store.LoadTail(ws, 1)
	if err != nil {
		t.Fatal(err)
	}
	events := tail.Transcripts["thread-a"]
	if len(events) != 2 || events[0].Text != "second" || events[1].Text != "reply two" {
		t.Fatalf("expected exactly the last turn to survive, got %+v", events)
	}
	if got := tail.Truncated["thread-a"]; got != 2 {
		t.Fatalf("Truncated = %d, want 2", got)
	}
}

func TestLoadTailLeavesBoundarylessTranscriptAlone(t *testing.T) {
	store := New(t.TempDir())
	defer store.Close()
	ws := "ws-tail-noboundary"
	if err := store.SaveMeta(Meta{Workspace: domain.Workspace{ID: ws}}); err != nil {
		t.Fatal(err)
	}
	appendTail(t, store, ws, "thread-a",
		tailEvent(domain.EventAgentMessage, "thread-a", "t1", "one", 1),
		tailEvent(domain.EventAgentMessage, "thread-a", "t1", "two", 2),
	)

	tail, err := store.LoadTail(ws, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Transcripts["thread-a"]) != 2 {
		t.Fatalf("transcript without a user boundary must not be cut, got %d", len(tail.Transcripts["thread-a"]))
	}
	if len(tail.Truncated) != 0 {
		t.Fatalf("expected no truncated threads, got %v", tail.Truncated)
	}
}

func TestLoadTailMergesContinuationThreads(t *testing.T) {
	store := New(t.TempDir())
	defer store.Close()
	ws := "ws-tail-cont"
	baseID := "task-base-1"
	contID := "task-base-1-cont-9999"

	meta := Meta{
		Workspace: domain.Workspace{ID: ws, Title: "Switch prompt", UpdatedAt: 100},
		Tasks: []domain.Task{
			{ThreadID: baseID, Title: "Switch prompt", CreatedAt: 100, UpdatedAt: 110},
			{ThreadID: contID, Title: "Switch prompt", CreatedAt: 120, UpdatedAt: 130},
		},
		Resume: map[string]string{baseID: "r1", contID: "r2"},
	}
	if err := store.SaveMeta(meta); err != nil {
		t.Fatal(err)
	}
	appendTail(t, store, ws, baseID,
		tailEvent(domain.EventUserMessage, baseID, "t1", "u1", 1),
		tailEvent(domain.EventAgentMessage, baseID, "t1", "a1", 2),
		tailEvent(domain.EventUserMessage, baseID, "t2", "u2", 3),
		tailEvent(domain.EventAgentMessage, baseID, "t2", "a2", 4),
	)
	appendTail(t, store, ws, contID,
		tailEvent(domain.EventUserMessage, contID, "t3", "u3", 5),
		tailEvent(domain.EventAgentMessage, contID, "t3", "a3", 6),
		tailEvent(domain.EventUserMessage, contID, "t4", "u4", 7),
		tailEvent(domain.EventAgentMessage, contID, "t4", "a4", 8),
	)
	if err := store.Flush(ws); err != nil {
		t.Fatal(err)
	}

	tail, err := store.LoadTail(ws, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tail.Transcripts[contID]; ok {
		t.Fatalf("continuation thread should have been merged into the root thread")
	}
	events := tail.Transcripts[baseID]
	if len(events) != 4 || events[0].Text != "u2" || events[2].Text != "u4" {
		t.Fatalf("unexpected merged tail: %+v", events)
	}
	if got := tail.Truncated[baseID]; got != 4 {
		t.Fatalf("Truncated = %d, want 4 (2 from each file)", got)
	}
	if _, ok := tail.Truncated[contID]; ok {
		t.Fatalf("continuation count should be folded into the root thread")
	}
	if len(tail.Meta.Tasks) != 1 || tail.Meta.Tasks[0].ThreadID != baseID {
		t.Fatalf("meta tasks were not merged: %+v", tail.Meta.Tasks)
	}
}
