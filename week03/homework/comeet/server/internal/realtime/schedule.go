package realtime

import (
	"context"
	"github.com/google/uuid"
	"time"
)

func (s *Service) schedulingLoop(ctx context.Context) {
	defer close(s.controlDone)
	initial, cancel := context.WithTimeout(ctx, 30*time.Second)
	_ = s.RetryControls(initial)
	cancel()
	seconds := s.Config.SweepSeconds
	if seconds <= 0 {
		seconds = 5
	}
	ticker := time.NewTicker(time.Duration(seconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run, release := context.WithTimeout(ctx, 15*time.Second)
			_ = s.Schedule(run, time.Now())
			release()
		}
	}
}

// Time is injectable for useful scheduler tests. Ending uses the same durable
// media cleanup path as a host action. No network call occurs in a DB transaction.
func (s *Service) Schedule(ctx context.Context, at time.Time) error {
	empty, maxDuration := s.Config.EmptySeconds, s.Config.MaxSeconds
	if empty == 0 {
		empty = s.Config.EmptyMinutes * 60
	}
	if maxDuration == 0 {
		maxDuration = s.Config.MaxMinutes * 60
	}
	if empty <= 0 && maxDuration <= 0 {
		return nil
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	now := at.UnixMilli()
	// Reconnecting members retain their full 45-second grace; valid reservations
	// also hold capacity. A process restart never resets a stored empty deadline.
	occupied := `EXISTS(SELECT 1 FROM participant_sessions p WHERE p.meeting_id=meetings.id AND (p.state='online' OR (p.state='reconnecting' AND p.disconnected_at>?) OR (p.state='joining' AND p.reserved_until>?)))`
	_, err := s.DB.ExecContext(ctx, "UPDATE meetings SET last_empty_at=CASE WHEN "+occupied+" THEN NULL ELSE COALESCE(last_empty_at,?) END WHERE status='active'", now-Grace.Milliseconds(), now, now)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT id,host_user_id FROM meetings WHERE status='active' AND ((?>0 AND started_at<=?) OR (?>0 AND last_empty_at<=?)) ORDER BY started_at LIMIT 100", maxDuration, now-int64(maxDuration)*1000, empty, now-int64(empty)*1000)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	type candidate struct{ id, host string }
	var due []candidate
	for rows.Next() {
		var v candidate
		if err = rows.Scan(&v.id, &v.host); err != nil {
			rows.Close()
			s.mu.Unlock()
			return err
		}
		due = append(due, v)
	}
	err = rows.Err()
	rows.Close()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	var first error
	for _, v := range due {
		s.mu.Lock()
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			s.mu.Unlock()
			return e
		}
		r, e := tx.ExecContext(ctx, "UPDATE meetings SET status='ending' WHERE id=? AND status='active' AND ((?>0 AND started_at<=?) OR (?>0 AND last_empty_at<=? AND NOT "+occupied+"))", v.id, maxDuration, now-int64(maxDuration)*1000, empty, now-int64(empty)*1000, now-Grace.Milliseconds(), now)
		changed := int64(0)
		if e == nil {
			changed, e = r.RowsAffected()
		}
		op := controlOp{uuid.NewString(), v.id, v.host, "end", "", "pending", now}
		if e == nil && changed > 0 {
			_, e = tx.ExecContext(ctx, "INSERT INTO moderation_operations("+controlColumns+") VALUES (?,?,?,?,?,?,?)", op.ID, op.MeetingID, op.Actor, op.Action, op.Target, op.State, op.Created)
		}
		if e == nil {
			e = tx.Commit()
		} else {
			tx.Rollback()
		}
		s.mu.Unlock()
		if e != nil {
			return e
		}
		if changed > 0 {
			if e = s.applyControl(ctx, op); e != nil && first == nil {
				first = e
			}
		}
	}
	return first
}
