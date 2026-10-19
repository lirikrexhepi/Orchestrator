package sqlite

import (
	"context"
	"fmt"
)

type migration struct {
	version int
	name    string
	sql     string
}

var migrations = []migration{
	{
		version: 1,
		name:    "initial_schema",
		sql: `
-- Workspaces
CREATE TABLE IF NOT EXISTS workspaces (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

-- Sessions (Agent Workers)
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    workspace_id TEXT REFERENCES workspaces(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    custom_name TEXT,
    project_path TEXT NOT NULL,
    branch TEXT,
    driver TEXT NOT NULL,
    model TEXT NOT NULL,
    cli_session_id TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'idle', 'completed', 'archived')),
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_workspace ON sessions(workspace_id);
CREATE INDEX IF NOT EXISTS idx_sessions_cli_session ON sessions(driver, cli_session_id);

-- Tasks (Session checklists and future cron/queue items)
CREATE TABLE IF NOT EXISTS tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'in_progress', 'done', 'completed', 'failed', 'cancelled')),
    priority TEXT NOT NULL DEFAULT 'normal' CHECK(priority IN ('low', 'normal', 'high')),
    order_index INTEGER NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'agent' CHECK(source IN ('agent', 'user', 'cron', 'queue')),
    scheduled_at DATETIME,
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_session_order ON tasks(session_id, order_index);
CREATE INDEX IF NOT EXISTS idx_tasks_status_scheduled ON tasks(status, scheduled_at);

-- Providers (Permissions, default models, and CLI paths)
CREATE TABLE IF NOT EXISTS providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    is_enabled INTEGER NOT NULL DEFAULT 1,
    preferred_model TEXT,
    binary_path TEXT,
    permission_granted_at DATETIME,
    updated_at DATETIME NOT NULL
);

-- User Preferences (Wallpapers, themes, window modes)
CREATE TABLE IF NOT EXISTS user_preferences (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at DATETIME NOT NULL
);

-- Projects
CREATE TABLE IF NOT EXISTS projects (
    path TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    is_git INTEGER NOT NULL DEFAULT 0,
    last_opened_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_projects_last_opened ON projects(last_opened_at DESC);
`,
	},
	{
		version: 2,
		name:    "activity_daily",
		sql: `
CREATE TABLE IF NOT EXISTS activity_daily (
    day TEXT NOT NULL,
    project TEXT NOT NULL DEFAULT '',
    driver TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    messages INTEGER NOT NULL DEFAULT 0,
    sessions INTEGER NOT NULL DEFAULT 0,
    tool_calls INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (day, project, driver, model)
);

CREATE INDEX IF NOT EXISTS idx_activity_daily_day ON activity_daily(day);
`,
	},
}

func (d *DB) migrate(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	for _, m := range migrations {
		var exists int
		row := d.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM schema_migrations WHERE version = ?", m.version)
		if err := row.Scan(&exists); err != nil {
			return fmt.Errorf("failed to check migration version %d: %w", m.version, err)
		}
		if exists > 0 {
			continue
		}

		tx, err := d.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration %d: %w", m.version, err)
		}

		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed executing migration %d (%s): %w", m.version, m.name, err)
		}

		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, name) VALUES (?, ?)", m.version, m.name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed recording migration %d: %w", m.version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %d: %w", m.version, err)
		}
	}

	return nil
}
