package site

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gocms/internal/db"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	accessLogQueueSize     = 2048
	accessLogBatchSize     = 100
	accessLogFlushInterval = 200 * time.Millisecond
	insertAccessLog        = `INSERT INTO "` + db.AccessLogTable + `" (` + db.AccessLogColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
)

type accessLogEvent struct {
	host, path, query, method, contentType, kind string
	ip, referer, userAgent, fetchSite            string
	status                                       int
	visited                                      time.Time
}

type accessLogWriter struct {
	queue   chan accessLogEvent
	done    chan struct{}
	cancel  context.CancelFunc
	mu      sync.Mutex
	closed  bool
	written atomic.Uint64
	busy    atomic.Uint64
	dropped [3]atomic.Uint64
}

func (s *Server) accessLogDB() *sql.DB {
	return s.accessLogDatabase
}

// StartAccessLogWriter opens the log store before serving HTTP.
func (s *Server) StartAccessLogWriter(ctx context.Context) error {
	if s.database == nil {
		return nil
	}
	cutoff := s.accessLogs.cutoff()
	database, err := db.OpenAccessLogs(ctx, s.database, cutoff)
	if err != nil {
		s.accessLogDatabase = nil
		return err
	}
	if database == nil {
		return nil
	}
	if err := backfillAccessLogCrawlers(ctx, database, cutoff); err != nil {
		log.Printf("reclassify retained access log crawlers: %v", err)
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	writer := &accessLogWriter{
		queue:  make(chan accessLogEvent, accessLogQueueSize),
		done:   make(chan struct{}),
		cancel: cancel,
	}
	s.accessLogDatabase, s.accessLogWriter = database, writer
	go writer.run(workerCtx, s)
	return nil
}

func backfillAccessLogCrawlers(ctx context.Context, database *sql.DB, cutoff string) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS gocms_schema_migration (name TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	marker, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO gocms_schema_migration (name) VALUES ('access-log-named-crawlers')`)
	if err != nil {
		return err
	}
	affected, err := marker.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		for _, pattern := range accessLogCrawlerBackfillPatterns {
			if _, err := tx.ExecContext(ctx, `UPDATE "`+db.AccessLogTable+`"
				SET "agent_kind" = ?, "agent_label" = ?, "device_kind" = 'other'
				WHERE "visited_at" >= ? AND "agent_kind" IN ('browser', 'other')
					AND instr(lower("user_agent"), ?) > 0`, pattern.kind, pattern.label, cutoff, pattern.match); err != nil {
				return fmt.Errorf("match access log crawler %s: %w", pattern.label, err)
			}
		}
	}
	return tx.Commit()
}

// CloseAccessLogWriter stops accepting events and flushes queued events after
// HTTP shutdown. The worker closes the store after it finishes.
func (s *Server) CloseAccessLogWriter(ctx context.Context) error {
	writer := s.accessLogWriter
	if writer == nil {
		return nil
	}
	writer.mu.Lock()
	if !writer.closed {
		writer.closed = true
		close(writer.queue)
	}
	writer.mu.Unlock()
	select {
	case <-writer.done:
		return nil
	case <-ctx.Done():
		writer.cancel()
		return ctx.Err()
	}
}

func accessLogField(value string, limit int) string {
	if len(value) > limit {
		value = strings.ToValidUTF8(value[:limit], "")
	}
	// Avoid retaining large HTTP request buffers through queued substrings.
	return strings.Clone(value)
}

func (event accessLogEvent) priority() int {
	if event.status >= 500 {
		return 2
	}
	if event.status == 404 {
		return 1
	}
	return 0
}

func (w *accessLogWriter) enqueue(event accessLogEvent) {
	priority := event.priority()
	w.mu.Lock()
	defer w.mu.Unlock()
	// Leave 384 slots for 404/5xx and reserve the final 128 for 5xx.
	reserved := [...]int{384, 128, 0}
	if w.closed || len(w.queue) >= cap(w.queue)-reserved[priority] {
		w.dropped[priority].Add(1)
		return
	}
	w.queue <- event
}

func (w *accessLogWriter) run(ctx context.Context, s *Server) {
	defer close(w.done)
	defer w.cancel()
	defer s.accessLogDatabase.Close()
	ticker := time.NewTicker(accessLogFlushInterval)
	defer ticker.Stop()
	batch := make([]accessLogEvent, 0, accessLogBatchSize)
	var lastErrorLog time.Time
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := w.flush(ctx, s, batch); err != nil && time.Since(lastErrorLog) >= time.Minute {
			lastErrorLog = time.Now()
			log.Printf("write access log batch: %v", err)
		}
		clear(batch)
		batch = batch[:0]
	}
	for {
		select {
		case event, ok := <-w.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, event)
			if len(batch) == accessLogBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (w *accessLogWriter) flush(ctx context.Context, s *Server, batch []accessLogEvent) error {
	prepareCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows := make([][]any, 0, len(batch))
	var rowEvents []accessLogEvent
	for _, event := range batch {
		args := s.accessLogEventArgs(prepareCtx, event)
		if args == nil {
			w.dropped[event.priority()].Add(1)
			continue
		}
		rows = append(rows, args)
		rowEvents = append(rowEvents, event)
	}
	if len(rows) == 0 {
		return prepareCtx.Err()
	}
	writeCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	var err error
	for attempt := range 3 {
		err = writeAccessLogRows(writeCtx, s.accessLogDB(), rows)
		if err == nil {
			w.written.Add(uint64(len(rows)))
			return nil
		}
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			break
		}
		code := sqliteErr.Code() & 0xff
		if code != sqlite3.SQLITE_BUSY && code != sqlite3.SQLITE_LOCKED {
			break
		}
		w.busy.Add(1)
		if writeCtx.Err() != nil || attempt == 2 {
			break
		}
		select {
		case <-writeCtx.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}
	for _, event := range rowEvents {
		w.dropped[event.priority()].Add(1)
	}
	return err
}

func (s *Server) accessLogEventArgs(ctx context.Context, event accessLogEvent) []any {
	if ctx.Err() != nil {
		return nil
	}
	siteID, domains, site := s.accessLogSite(ctx, event.host)
	if siteID <= 0 {
		return nil
	}
	refererHost := refererHostName(event.referer)
	refererKind := s.classifyAccessLogReferer(ctx, event.host, event.fetchSite, site, siteID, domains, refererHost, event.referer, event.status)
	agent := summarizeUserAgent(event.userAgent)
	return []any{siteID, event.host, s.accessLogs.dayKey(event.visited), event.path, event.query, event.method,
		event.status, event.kind, event.contentType, event.ip, event.referer, refererHost, refererKind, event.userAgent,
		agent.Kind, agent.Label, agent.Device, event.visited.UTC().Format(accessLogTimestampLayout)}
}

func writeAccessLogRows(ctx context.Context, database *sql.DB, rows [][]any) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	statement, err := transaction.PrepareContext(ctx, insertAccessLog)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, args := range rows {
		if _, err := statement.ExecContext(ctx, args...); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

// StartAccessLogMetrics samples connection-pool waits separately from SQLite
// write-lock waits. Public request latency can be compared in proxy logs.
func (s *Server) StartAccessLogMetrics(ctx context.Context) {
	if s.database == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		business := s.database.Stats()
		database := s.accessLogDB()
		var logs sql.DBStats
		if database != nil {
			logs = database.Stats()
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current := s.database.Stats()
				businessWaits, businessWait := current.WaitCount-business.WaitCount, current.WaitDuration-business.WaitDuration
				business = current
				var logWaits int64
				var logWait time.Duration
				if database != nil && database != s.database {
					current := database.Stats()
					logWaits, logWait = current.WaitCount-logs.WaitCount, current.WaitDuration-logs.WaitDuration
					logs = current
				}
				queueMetrics := ""
				if w := s.accessLogWriter; w != nil {
					queueMetrics = fmt.Sprintf(" queue_depth=%d written_total=%d sqlite_busy_total=%d dropped_other_total=%d dropped_404_total=%d dropped_5xx_total=%d",
						len(w.queue), w.written.Load(), w.busy.Load(), w.dropped[0].Load(), w.dropped[1].Load(), w.dropped[2].Load())
				}
				log.Printf("database pool (1m): business_waits=%d business_wait=%s access_waits=%d access_wait=%s%s",
					businessWaits, businessWait, logWaits, logWait, queueMetrics)
			}
		}
	}()
}
