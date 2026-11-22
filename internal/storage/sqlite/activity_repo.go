package sqlite

import (
	"context"
	"fmt"

	"composer/internal/activity"
)

type ActivityRepo struct {
	db *DB
}

func NewActivityRepo(db *DB) *ActivityRepo {
	return &ActivityRepo{db: db}
}

func (r *ActivityRepo) AddActivity(ctx context.Context, rows []activity.Row) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := r.db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin activity write: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO activity_daily (day, project, driver, model, messages, sessions, agents, working_ms, tool_calls, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(day, project, driver, model) DO UPDATE SET
			messages = activity_daily.messages + excluded.messages,
			sessions = activity_daily.sessions + excluded.sessions,
			agents = activity_daily.agents + excluded.agents,
			working_ms = activity_daily.working_ms + excluded.working_ms,
			tool_calls = activity_daily.tool_calls + excluded.tool_calls,
			input_tokens = activity_daily.input_tokens + excluded.input_tokens,
			output_tokens = activity_daily.output_tokens + excluded.output_tokens,
			cache_read_tokens = activity_daily.cache_read_tokens + excluded.cache_read_tokens,
			cache_write_tokens = activity_daily.cache_write_tokens + excluded.cache_write_tokens;
	`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed to prepare activity write: %w", err)
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx,
			row.Day, row.Project, row.Driver, row.Model,
			row.Messages, row.Sessions, row.Agents, row.WorkingMs, row.ToolCalls,
			row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheWriteTokens,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to write activity for %s: %w", row.Day, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit activity: %w", err)
	}
	return nil
}

// BackfillAgentsAndWorking populates only the agents and working_ms columns
// from historical transcripts without double-counting messages/sessions/tokens.
func (r *ActivityRepo) BackfillAgentsAndWorking(ctx context.Context, rows []activity.Row) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := r.db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin activity metrics backfill: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO activity_daily (day, project, driver, model, agents, working_ms)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(day, project, driver, model) DO UPDATE SET
			agents = MAX(activity_daily.agents, excluded.agents),
			working_ms = MAX(activity_daily.working_ms, excluded.working_ms);
	`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed to prepare activity metrics backfill: %w", err)
	}
	defer stmt.Close()
	for _, row := range rows {
		if row.Agents == 0 && row.WorkingMs == 0 {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			row.Day, row.Project, row.Driver, row.Model,
			row.Agents, row.WorkingMs,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to backfill activity metrics for %s: %w", row.Day, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit activity metrics backfill: %w", err)
	}
	return nil
}

func (r *ActivityRepo) Rows(ctx context.Context) ([]activity.Row, error) {
	rows, err := r.db.db.QueryContext(ctx, `
		SELECT day, project, driver, model, messages, sessions, agents, working_ms, tool_calls, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens
		FROM activity_daily ORDER BY day;
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to read activity: %w", err)
	}
	defer rows.Close()

	var out []activity.Row
	for rows.Next() {
		var row activity.Row
		if err := rows.Scan(
			&row.Day, &row.Project, &row.Driver, &row.Model,
			&row.Messages, &row.Sessions, &row.Agents, &row.WorkingMs, &row.ToolCalls,
			&row.InputTokens, &row.OutputTokens, &row.CacheReadTokens, &row.CacheWriteTokens,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
