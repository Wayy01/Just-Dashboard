package deploy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type EventInput struct {
	Type   EventType
	StepID int64
	Stream string
	Text   string
	Data   json.RawMessage
}

func appendEventTx(
	ctx context.Context,
	tx *sql.Tx,
	now time.Time,
	runID, stepID int64,
	eventType EventType,
	stream, text string,
	data json.RawMessage,
) (RunEvent, error) {
	if !validEventType(eventType) || eventType == EventResync {
		return RunEvent{}, fmt.Errorf("invalid persisted deployment event type %q", eventType)
	}
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	if !json.Valid(data) {
		return RunEvent{}, fmt.Errorf("deployment event data is not valid JSON")
	}
	if len(data) > maxEventBytes {
		return RunEvent{}, ErrEventPayloadTooLarge
	}
	text, truncated := truncateUTF8(text, maxLogTextBytes)
	var seq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM deploy_log_chunks WHERE run_id = ?`,
		runID).Scan(&seq); err != nil {
		return RunEvent{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO deploy_log_chunks(
		  run_id, step_id, seq, event_type, stream, ts, text, data_json, truncated)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, stepID, seq, eventType, stream, now.Unix(), text, string(data), boolInt(truncated)); err != nil {
		return RunEvent{}, err
	}
	return RunEvent{Seq: seq, Type: eventType, RunID: runID, StepID: stepID, TS: now, Data: data}, nil
}

// AppendEvent appends feature evidence only while the caller owns the active
// lease. State transitions have dedicated methods so they cannot be smuggled
// through a generic event write.
func (s *OrchestrationStore) AppendEvent(
	ctx context.Context,
	runID int64,
	claimToken string,
	input EventInput,
) (RunEvent, error) {
	if input.Type == EventRunState || input.Type == EventStepState || input.Type == EventResync {
		return RunEvent{}, fmt.Errorf("event type %q requires its state method", input.Type)
	}
	if input.Type == EventStepLog {
		return s.AppendLog(ctx, runID, input.StepID, claimToken, input.Stream, input.Text)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RunEvent{}, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	if err := assertLeaseTx(ctx, tx, runID, claimToken, now); err != nil {
		return RunEvent{}, err
	}
	event, err := appendEventTx(ctx, tx, now, runID, input.StepID, input.Type,
		input.Stream, input.Text, input.Data)
	if err != nil {
		return RunEvent{}, err
	}
	if input.StepID != 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE deploy_steps SET last_seq = ? WHERE id = ? AND run_id = ?`,
			event.Seq, input.StepID, runID); err != nil {
			return RunEvent{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return RunEvent{}, err
	}
	s.publish([]RunEvent{event})
	return event, nil
}

func (s *OrchestrationStore) AppendLog(
	ctx context.Context,
	runID, stepID int64,
	claimToken, stream, text string,
) (RunEvent, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if stream != "stdout" && stream != "stderr" && stream != "status" {
		return RunEvent{}, fmt.Errorf("invalid deployment log stream %q", stream)
	}
	text, truncated := truncateUTF8(text, maxLogTextBytes)
	data := logEventData(stream, &text, &truncated)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RunEvent{}, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	if err := assertLeaseTx(ctx, tx, runID, claimToken, now); err != nil {
		return RunEvent{}, err
	}
	if stepID != 0 {
		var exists int
		if err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM deploy_steps WHERE id = ? AND run_id = ?`, stepID, runID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return RunEvent{}, ErrStepNotFound
			}
			return RunEvent{}, err
		}
	}
	event, err := appendEventTx(ctx, tx, now, runID, stepID, EventStepLog, stream, text, data)
	if err != nil {
		return RunEvent{}, err
	}
	if stepID != 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE deploy_steps SET last_seq = ? WHERE id = ?`, event.Seq, stepID); err != nil {
			return RunEvent{}, err
		}
	}
	// The old deployment detail response reads deploy_runs.log. Keep a bounded
	// tail while 0.6.6 clients coexist; sequenced chunks remain authoritative.
	if _, err := tx.ExecContext(ctx, `
		UPDATE deploy_runs
		   SET log = CASE
		     WHEN length(log) + length(?) <= 64000 THEN log || ?
		     ELSE '…' || substr(log || ?, -64000)
		   END
		 WHERE id = ?`, text, text, text, runID); err != nil {
		return RunEvent{}, err
	}
	if err := tx.Commit(); err != nil {
		return RunEvent{}, err
	}
	s.publish([]RunEvent{event})
	return event, nil
}

func logEventData(stream string, text *string, truncated *bool) json.RawMessage {
	for {
		data := mustJSON(map[string]any{
			"stream": stream, "text": *text, "truncated": *truncated,
		})
		if len(data) <= maxEventBytes {
			return data
		}
		// JSON escaping can make control-heavy output several times larger than
		// its persisted text. Reduce by bytes while retaining a complete rune;
		// the event envelope's bound is as important as the text-column bound.
		limit := len(*text) * 3 / 4
		if limit == len(*text) {
			limit--
		}
		*text, _ = truncateUTF8(*text, limit)
		*truncated = true
	}
}

// EventsAfter returns retained events in sequence order. If compaction removed
// payload newer than after, the first event is an in-memory resync snapshot;
// resync is never persisted into the run sequence itself.
func (s *OrchestrationStore) EventsAfter(
	ctx context.Context,
	runID, after int64,
	limit int,
) ([]RunEvent, error) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	if _, err := s.Run(ctx, runID); err != nil {
		return nil, err
	}
	var watermark int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(seq), 0) FROM deploy_log_chunks
		 WHERE run_id = ? AND compacted = 1`, runID).Scan(&watermark); err != nil {
		return nil, err
	}
	result := make([]RunEvent, 0, limit+1)
	start := after
	if after < watermark {
		snapshot, err := s.Snapshot(ctx, runID)
		if err != nil {
			return nil, err
		}
		var oldest int64
		if err := s.db.QueryRowContext(ctx, `
			SELECT COALESCE(MIN(seq), 0) FROM deploy_log_chunks
			 WHERE run_id = ? AND seq > ? AND compacted = 0`, runID, watermark).Scan(&oldest); err != nil {
			return nil, err
		}
		result = append(result, RunEvent{
			Seq: watermark, Type: EventResync, RunID: runID, TS: s.now().UTC(),
			Data: mustJSON(map[string]any{"oldestSeq": oldest, "snapshot": snapshot}),
		})
		start = watermark
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT seq, step_id, event_type, ts, data_json
		  FROM deploy_log_chunks
		 WHERE run_id = ? AND seq > ? AND compacted = 0
		 ORDER BY seq
		 LIMIT ?`, runID, start, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var event RunEvent
		var ts int64
		var data string
		event.RunID = runID
		if err := rows.Scan(&event.Seq, &event.StepID, &event.Type, &ts, &data); err != nil {
			return nil, err
		}
		event.TS = unixTime(ts)
		event.Data = json.RawMessage(data)
		result = append(result, event)
	}
	return result, rows.Err()
}

// CompactRunLogs retains the newest log payloads and one sequence watermark.
// State/evidence/artifact events are never selected. The watermark makes a
// reconnect request prove that it needs a fresh snapshot even when retained
// state events have older sequence numbers.
func (s *OrchestrationStore) CompactRunLogs(ctx context.Context, runID int64, keep int) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if keep < 0 {
		return fmt.Errorf("negative deployment log retention")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var terminal string
	if err := tx.QueryRowContext(ctx,
		`SELECT state FROM deploy_runs WHERE id = ?`, runID).Scan(&terminal); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRunNotFound
		}
		return err
	}
	if !RunState(terminal).Terminal() {
		return fmt.Errorf("deployment logs compact only after the run is terminal")
	}
	var watermark int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(seq), 0) FROM deploy_log_chunks
		 WHERE run_id = ? AND event_type = ? AND compacted = 0
		   AND seq NOT IN (
		     SELECT seq FROM deploy_log_chunks
		      WHERE run_id = ? AND event_type = ? AND compacted = 0
		      ORDER BY seq DESC LIMIT ?
		   )`, runID, EventStepLog, runID, EventStepLog, keep).Scan(&watermark); err != nil {
		return err
	}
	if watermark == 0 {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM deploy_log_chunks
		 WHERE run_id = ? AND event_type = ? AND compacted = 0 AND seq < ?`,
		runID, EventStepLog, watermark); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE deploy_log_chunks
		   SET compacted = 1, stream = 'status', text = '', data_json = '{}',
		       redacted = 0, truncated = 0
		 WHERE run_id = ? AND seq = ?`, runID, watermark); err != nil {
		return err
	}
	return tx.Commit()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
