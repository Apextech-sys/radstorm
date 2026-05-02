// Package runs — file tailing for SSE.
//
// Purpose:
//
//	TailLines polls a JSONL file (typically progress.jsonl) and delivers
//	each newly appended line on the returned channel. The CLI writes one
//	JSON object per line; this helper handles file-doesn't-exist-yet,
//	partial-line-not-yet-flushed, and rotation/truncation safely.
//
//	The polling interval defaults to 200ms which is the SSE cadence the
//	briefing calls out. Done is closed when the caller's context is
//	cancelled or when the stop signal is closed.
//
// Related files:
//   - apps/api/internal/server/handlers/events.go (consumer)
//   - apps/api/internal/runs/runner.go (writes the directory the CLI
//     fills with progress.jsonl)
//   - .orchestration/contracts/results-schema.md
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: internal helper.
package runs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"time"
)

// DefaultTailInterval is the polling cadence for TailLines. 200ms keeps
// SSE perceptibly real-time without thrashing the disk.
var DefaultTailInterval = 200 * time.Millisecond

// TailOptions configures TailLines.
type TailOptions struct {
	// Interval between stat() polls. If zero, DefaultTailInterval is used.
	Interval time.Duration
	// Stop is an optional auxiliary stop signal (e.g. close when the
	// run is finished). nil means rely on ctx.Done() only.
	Stop <-chan struct{}
}

// TailLines polls the file at path and emits each newly-appended line on
// the returned channel. The channel is closed when ctx is cancelled, the
// optional Stop channel fires, or a non-recoverable read error occurs.
//
// Lines are emitted as raw byte slices (without the trailing newline).
// Callers are expected to JSON-decode them.
//
// While the file does not yet exist, TailLines polls quietly.
func TailLines(ctx context.Context, path string, opts TailOptions) <-chan []byte {
	out := make(chan []byte, 16)
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultTailInterval
	}

	go func() {
		defer close(out)
		var (
			f         *os.File
			reader    *bufio.Reader
			lastSize  int64
			lineBuf   bytes.Buffer
		)
		closeFile := func() {
			if f != nil {
				_ = f.Close()
				f = nil
				reader = nil
			}
		}
		defer closeFile()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			// Try to open if not open yet.
			if f == nil {
				openF, err := os.Open(path)
				if err == nil {
					f = openF
					reader = bufio.NewReader(f)
					lastSize = 0
				}
			} else {
				// Detect truncation: if the on-disk size is smaller than
				// what we've already read, reopen from start.
				if info, err := os.Stat(path); err == nil {
					if info.Size() < lastSize {
						closeFile()
						lineBuf.Reset()
						continue
					}
				}
			}

			// Drain whatever is currently available without blocking on
			// missing data.
			if reader != nil {
				for {
					chunk, err := reader.ReadSlice('\n')
					if len(chunk) > 0 {
						lineBuf.Write(chunk)
						if chunk[len(chunk)-1] == '\n' {
							line := lineBuf.Bytes()
							// Strip trailing CR/LF.
							line = bytes.TrimRight(line, "\r\n")
							if len(line) > 0 {
								// Copy because lineBuf will be reset.
								cp := make([]byte, len(line))
								copy(cp, line)
								select {
								case out <- cp:
								case <-ctx.Done():
									return
								case <-opts.Stop:
									return
								}
							}
							lineBuf.Reset()
						}
					}
					if err != nil {
						if errors.Is(err, io.EOF) || errors.Is(err, bufio.ErrBufferFull) {
							break
						}
						// Unexpected error; stop tailing.
						return
					}
				}
				if info, statErr := f.Stat(); statErr == nil {
					lastSize = info.Size()
				}
			}

			// Wait for next tick or shutdown.
			select {
			case <-ctx.Done():
				return
			case <-opts.Stop:
				return
			case <-ticker.C:
			}
		}
	}()
	return out
}
