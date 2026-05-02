// Package collector — Parquet writer wrapper used by each collector shard.
//
// Purpose:
//
//	Wraps github.com/parquet-go/parquet-go's GenericWriter so each shard
//	can lazily open files, append batches of events, rotate when a file
//	exceeds the configured size cap, and flush on shutdown. One writer
//	instance per shard; not safe for concurrent use within a shard.
//
// Related files:
//   - pkg/collector/collector.go (owns one parquetWriter per shard)
//   - pkg/events/event.go (the row type)
//   - .orchestration/contracts/event-schema.md (schema this writer emits)
//
// Briefing: .orchestration/briefings/1c-events.md
//
// Contract: internal — package-private writer.
package collector

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Apextech-sys/radstorm/pkg/events"
	"github.com/parquet-go/parquet-go"
)

// rotateBytesDefault — default file-rotate threshold (256 MiB per
// event-schema.md "Periodic flush" guidance).
const rotateBytesDefault int64 = 256 * 1024 * 1024

// parquetWriter owns the file handle + Generic[Event] writer for a single
// shard. Files are named events-shard-<shard>-<seq>.parquet, sequence
// increments on rotation. Files for shards that emitted no events are
// never opened.
type parquetWriter struct {
	dir         string
	shardID     int
	rotateBytes int64

	currentSeq    int
	currentFile   *os.File
	currentWriter *parquet.GenericWriter[events.Event]
	bytesWritten  int64
	totalRows     int64

	closed bool
}

// newParquetWriter returns a writer that has not yet opened any file. The
// first call to writeBatch opens events-shard-<shard>-0.parquet.
func newParquetWriter(dir string, shardID int, rotateBytes int64) *parquetWriter {
	if rotateBytes <= 0 {
		rotateBytes = rotateBytesDefault
	}
	return &parquetWriter{dir: dir, shardID: shardID, rotateBytes: rotateBytes}
}

// writeBatch appends rows to the current file, opening one if necessary
// and rotating before writing if the current file would exceed the
// configured size cap.
func (w *parquetWriter) writeBatch(rows []events.Event) error {
	if w.closed {
		return errors.New("parquet writer: write after close")
	}
	if len(rows) == 0 {
		return nil
	}

	if w.currentWriter == nil {
		if err := w.openNext(); err != nil {
			return err
		}
	}

	// Best-effort rotation check before writing. Size estimation is
	// approximate (compressed chunks), but the cap is itself a soft
	// guideline so this is acceptable.
	if w.bytesWritten >= w.rotateBytes {
		if err := w.rotate(); err != nil {
			return err
		}
	}

	n, err := w.currentWriter.Write(rows)
	if err != nil {
		return fmt.Errorf("parquet write: %w", err)
	}
	w.totalRows += int64(n)
	w.bytesWritten = w.currentWriter.Size()
	return nil
}

// openNext opens events-shard-<shard>-<currentSeq>.parquet, prepares a
// fresh GenericWriter against it.
func (w *parquetWriter) openNext() error {
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", w.dir, err)
	}
	name := fmt.Sprintf("events-shard-%d-%d.parquet", w.shardID, w.currentSeq)
	path := filepath.Join(w.dir, name)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	w.currentFile = f
	w.currentWriter = parquet.NewGenericWriter[events.Event](f)
	w.bytesWritten = 0
	return nil
}

// rotate flushes + closes the current file and opens the next one.
func (w *parquetWriter) rotate() error {
	if err := w.closeCurrent(); err != nil {
		return err
	}
	w.currentSeq++
	return w.openNext()
}

// closeCurrent flushes the current writer + fsyncs + closes the file
// handle. Safe to call when no file is open (no-op).
func (w *parquetWriter) closeCurrent() error {
	if w.currentWriter == nil {
		return nil
	}
	if err := w.currentWriter.Close(); err != nil {
		return fmt.Errorf("parquet close: %w", err)
	}
	if w.currentFile != nil {
		_ = w.currentFile.Sync()
		if err := w.currentFile.Close(); err != nil {
			return fmt.Errorf("file close: %w", err)
		}
	}
	w.currentWriter = nil
	w.currentFile = nil
	return nil
}

// close flushes + closes whatever is open and refuses further writes.
func (w *parquetWriter) close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.closeCurrent()
}

// outcomeWriter is the SubscriberOutcome equivalent of parquetWriter. We
// keep them separate (different schemas, different row types) but the
// shape of the API is intentionally identical.
type outcomeWriter struct {
	path     string
	file     *os.File
	writer   *parquet.GenericWriter[events.SubscriberOutcome]
	rows     int64
	closed   bool
	openedAt bool
}

func newOutcomeWriter(dir string) *outcomeWriter {
	return &outcomeWriter{path: filepath.Join(dir, "subscribers.parquet")}
}

func (w *outcomeWriter) writeBatch(rows []events.SubscriberOutcome) error {
	if w.closed {
		return errors.New("outcome writer: write after close")
	}
	if len(rows) == 0 {
		return nil
	}
	if !w.openedAt {
		if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
		f, err := os.Create(w.path)
		if err != nil {
			return fmt.Errorf("create %s: %w", w.path, err)
		}
		w.file = f
		w.writer = parquet.NewGenericWriter[events.SubscriberOutcome](f)
		w.openedAt = true
	}
	n, err := w.writer.Write(rows)
	if err != nil {
		return fmt.Errorf("outcome write: %w", err)
	}
	w.rows += int64(n)
	return nil
}

func (w *outcomeWriter) close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.writer == nil {
		return nil
	}
	if err := w.writer.Close(); err != nil {
		return fmt.Errorf("outcome parquet close: %w", err)
	}
	if w.file != nil {
		_ = w.file.Sync()
		if err := w.file.Close(); err != nil {
			return fmt.Errorf("outcome file close: %w", err)
		}
	}
	return nil
}
