package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"composer/internal/activity"
	"composer/internal/domain"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func TestSessionRepo(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := NewSessionRepo(db)

	session := domain.SessionRecord{
		ID:          "thread-1",
		Title:       "Initial Task",
		ProjectPath: "C:/Projects/test",
		Driver:      "claude",
		Model:       "claude-3-7-sonnet",
		Status:      "active",
	}

	if err := repo.Save(ctx, session); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	got, err := repo.Get(ctx, "thread-1")
	if err != nil || got == nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if got.Title != "Initial Task" {
		t.Errorf("expected Title 'Initial Task', got '%s'", got.Title)
	}
	if got.DisplayName() != "Initial Task" {
		t.Errorf("expected DisplayName 'Initial Task', got '%s'", got.DisplayName())
	}

	// Rename session
	if err := repo.Rename(ctx, "thread-1", "Auth Feature"); err != nil {
		t.Fatalf("failed to rename session: %v", err)
	}

	gotRenamed, err := repo.Get(ctx, "thread-1")
	if err != nil || gotRenamed == nil {
		t.Fatalf("failed to get renamed session: %v", err)
	}
	if gotRenamed.CustomName != "Auth Feature" {
		t.Errorf("expected CustomName 'Auth Feature', got '%s'", gotRenamed.CustomName)
	}
	if gotRenamed.DisplayName() != "Auth Feature" {
		t.Errorf("expected DisplayName 'Auth Feature', got '%s'", gotRenamed.DisplayName())
	}

	// Update CLI session ID
	if err := repo.UpdateCLISession(ctx, "thread-1", "claude-uuid-99"); err != nil {
		t.Fatalf("failed to update CLI session: %v", err)
	}
	gotCLI, _ := repo.Get(ctx, "thread-1")
	if gotCLI.CLISessionID != "claude-uuid-99" {
		t.Errorf("expected CLISessionID 'claude-uuid-99', got '%s'", gotCLI.CLISessionID)
	}
}

func TestTaskRepo(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	sRepo := NewSessionRepo(db)
	tRepo := NewTaskRepo(db)

	sessID := "thread-tasks"
	_ = sRepo.Save(ctx, domain.SessionRecord{
		ID:          sessID,
		Title:       "Task Session",
		ProjectPath: "C:/Projects/test",
		Driver:      "antigravity",
		Model:       "gemini-3.8-flash",
		Status:      "active",
	})

	initialTasks := []domain.TaskRecord{
		{Content: "Setup database", Status: "pending"},
		{Content: "Create schema", Status: "pending"},
		{Content: "Write tests", Status: "pending"},
	}

	if err := tRepo.SetSessionTasks(ctx, sessID, initialTasks); err != nil {
		t.Fatalf("failed to set session tasks: %v", err)
	}

	tasks, err := tRepo.ListBySession(ctx, sessID)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if tasks[0].Content != "Setup database" || tasks[0].OrderIndex != 0 {
		t.Errorf("task 0 mismatch: %+v", tasks[0])
	}

	// Update status by target string
	if err := tRepo.UpdateStatusBySessionAndTarget(ctx, sessID, "Setup database", "done"); err != nil {
		t.Fatalf("failed to update status by target: %v", err)
	}

	updatedTasks, _ := tRepo.ListBySession(ctx, sessID)
	if updatedTasks[0].Status != "done" || updatedTasks[0].CompletedAt == nil {
		t.Errorf("expected task 0 to be done with completedAt set: %+v", updatedTasks[0])
	}

	// Add new task
	sid := sessID
	_, err = tRepo.AddTask(ctx, domain.TaskRecord{
		SessionID: &sid,
		Content:   "Deploy release",
		Status:    "pending",
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	allTasks, _ := tRepo.ListBySession(ctx, sessID)
	if len(allTasks) != 4 {
		t.Fatalf("expected 4 tasks after add, got %d", len(allTasks))
	}
	if allTasks[3].OrderIndex != 3 {
		t.Errorf("expected order index 3, got %d", allTasks[3].OrderIndex)
	}

	// Test CASCADE delete: deleting session removes all tasks
	if err := sRepo.Delete(ctx, sessID); err != nil {
		t.Fatalf("failed to delete session: %v", err)
	}
	afterDeleteTasks, _ := tRepo.ListBySession(ctx, sessID)
	if len(afterDeleteTasks) != 0 {
		t.Errorf("expected 0 tasks after session delete (cascade), got %d", len(afterDeleteTasks))
	}
}

func TestProviderRepo(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := NewProviderRepo(db)

	if err := repo.SetPreferredModel(ctx, "claude", "claude-opus-5"); err != nil {
		t.Fatalf("failed to set preferred model: %v", err)
	}

	p, err := repo.Get(ctx, "claude")
	if err != nil || p == nil {
		t.Fatalf("failed to get provider: %v", err)
	}
	if p.PreferredModel != "claude-opus-5" {
		t.Errorf("expected 'claude-opus-5', got '%s'", p.PreferredModel)
	}

	if err := repo.SetPermission(ctx, "claude", true); err != nil {
		t.Fatalf("failed to set permission: %v", err)
	}
	pPerm, _ := repo.Get(ctx, "claude")
	if !pPerm.IsEnabled || pPerm.PermissionGrantedAt == nil {
		t.Errorf("expected enabled with permission timestamp: %+v", pPerm)
	}
}

func TestPreferenceAndProjectRepo(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	prefRepo := NewPreferenceRepo(db)
	projRepo := NewProjectRepo(db)

	// Preference
	if err := prefRepo.Set(ctx, "wallpaper", "wallpaper2.jpg"); err != nil {
		t.Fatalf("failed to set preference: %v", err)
	}
	val, err := prefRepo.Get(ctx, "wallpaper")
	if err != nil || val != "wallpaper2.jpg" {
		t.Errorf("expected wallpaper2.jpg, got '%s'", val)
	}

	// Project
	now := time.Now()
	if err := projRepo.Upsert(ctx, domain.ProjectRecord{
		Path:         "C:/Projects/composer",
		Name:         "composer",
		IsGit:        true,
		LastOpenedAt: now,
	}); err != nil {
		t.Fatalf("failed to upsert project: %v", err)
	}

	projects, err := projRepo.List(ctx)
	if err != nil || len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].Name != "composer" || !projects[0].IsGit {
		t.Errorf("project mismatch: %+v", projects[0])
	}
}

func TestActivityRepoAccumulates(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := NewActivityRepo(db)

	row := activity.Row{
		Key:    activity.Key{Day: "2026-10-12", Project: "/p", Driver: "claude", Model: "opus"},
		Counts: activity.Counts{Messages: 2, InputTokens: 10},
	}
	if err := repo.AddActivity(ctx, []activity.Row{row, row}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.Rows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Messages != 4 || rows[0].InputTokens != 20 {
		t.Fatalf("got %+v", rows)
	}
}
