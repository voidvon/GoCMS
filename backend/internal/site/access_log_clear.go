package site

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"gocms/internal/db"
)

const accessLogClearPause = 50 * time.Millisecond

var errAccessLogClearRunning = errors.New("another access log clear is running")

type accessLogClearStatus struct {
	State   string `json:"state"`
	Deleted int64  `json:"deleted"`
}

type accessLogClearJob struct {
	accessLogClearStatus
	done chan struct{}
}

type accessLogClearJobs struct {
	mu     sync.Mutex
	ctx    context.Context
	active *accessLogClearJob
	latest map[int64]*accessLogClearJob
}

func (jobs *accessLogClearJobs) status(siteID int64) *accessLogClearStatus {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if job := jobs.latest[siteID]; job != nil {
		status := job.accessLogClearStatus
		return &status
	}
	return nil
}

func (s *Server) startAccessLogClear(ctx context.Context, siteID int64, filter AccessLogFilter) (*accessLogClearJob, error) {
	jobs := &s.accessLogClear
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if jobs.active != nil {
		return nil, errAccessLogClearRunning
	}
	operationCtx := jobs.ctx
	if operationCtx == nil {
		operationCtx = context.Background()
	}
	if err := operationCtx.Err(); err != nil {
		return nil, err
	}
	// Freeze the window and upper ID before acknowledging the operation.
	where, args := s.buildAccessLogWhereAt(siteID, normalizeAccessLogFilter(filter), time.Now())
	var upperID int64
	if err := s.accessLogDB().QueryRowContext(ctx, `SELECT COALESCE(MAX("id"), 0) FROM "`+db.AccessLogTable+`" WHERE "site_id" = ?`, siteID).Scan(&upperID); err != nil {
		return nil, err
	}
	job := &accessLogClearJob{accessLogClearStatus: accessLogClearStatus{State: "running"}, done: make(chan struct{})}
	if jobs.latest == nil {
		jobs.latest = make(map[int64]*accessLogClearJob)
	}
	jobs.latest[siteID] = job
	jobs.active = job
	go func() {
		err := s.runAccessLogClear(operationCtx, siteID, upperID, where, args, func(deleted int64) {
			jobs.mu.Lock()
			job.Deleted += deleted
			jobs.mu.Unlock()
		})
		jobs.mu.Lock()
		defer jobs.mu.Unlock()
		if job.Deleted > 0 {
			s.dashboardStats.invalidate(siteID)
		}
		job.State = "completed"
		if err != nil {
			job.State = "failed"
			log.Printf("clear access logs: site=%d deleted=%d: %v", siteID, job.Deleted, err)
		}
		jobs.active = nil
		close(job.done)
	}()
	return job, nil
}

func (s *Server) runAccessLogClear(ctx context.Context, siteID, upperID int64, where string, args []any, progress func(int64)) error {
	const table = `"` + db.AccessLogTable + `"`
	database := s.accessLogDB()
	for cursor := int64(0); cursor < upperID; {
		batchCtx, cancel := context.WithTimeout(ctx, accessLogCleanupBatchTimeout)
		started := time.Now()
		var end int64
		err := database.QueryRowContext(batchCtx, `SELECT COALESCE(MAX("id"), 0) FROM (
			SELECT "id" FROM `+table+` WHERE "site_id" = ? AND "id" > ? AND "id" <= ?
			ORDER BY "id" LIMIT ?
		)`, siteID, cursor, upperID, accessLogCleanupBatchSize).Scan(&end)
		if err != nil || end == 0 {
			cancel()
			return err
		}
		batchArgs := append(append([]any{}, args...), cursor, end)
		// Apply arbitrary filters to a bounded range of candidate rows.
		result, err := database.ExecContext(batchCtx, `DELETE FROM `+table+` WHERE "id" IN (
			SELECT "id" FROM `+table+` INDEXED BY idx_gocms_access_log_site_time
			WHERE `+where+` AND "id" > ? AND "id" <= ?
		)`, batchArgs...)
		cancel()
		if err != nil {
			return err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if deleted > 0 {
			progress(deleted)
		}
		if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
			log.Printf("slow access log clear batch: site=%d duration=%s deleted=%d", siteID, elapsed, deleted)
		}
		cursor = end
		if cursor < upperID {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(accessLogClearPause):
			}
		}
	}
	return nil
}
