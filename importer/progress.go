package importer

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// StorePhase is where a store is in the import.
type StorePhase int32

const (
	StoreWaiting StorePhase = iota
	StoreCopying
	StoreVerifying
	StoreDone
)

// StoreProgress is a snapshot of one store's import.
type StoreProgress struct {
	Name    string
	Phase   StorePhase
	Total   int64 // leaves recorded in the tree root
	Done    int64 // leaves written so far
	Elapsed time.Duration
}

// progressTracker holds per-store counters that workers update lock-free and
// a reporter reads periodically.
type progressTracker struct {
	names  []string
	stores map[string]*storeCounters
}

type storeCounters struct {
	phase atomic.Int32
	total atomic.Int64
	done  atomic.Int64
	start atomic.Int64 // unix nanoseconds
	end   atomic.Int64 // unix nanoseconds
}

func newProgressTracker(names []string, totals []int64) *progressTracker {
	t := &progressTracker{names: names, stores: make(map[string]*storeCounters, len(names))}
	for i, name := range names {
		s := &storeCounters{}
		s.total.Store(totals[i])
		t.stores[name] = s
	}
	return t
}

// started marks a store as copying and returns its leaf total.
func (t *progressTracker) started(name string) int64 {
	s := t.stores[name]
	s.start.Store(time.Now().UnixNano())
	s.phase.Store(int32(StoreCopying))
	return s.total.Load()
}

func (t *progressTracker) wrote(name string, done int64) {
	t.stores[name].done.Store(done)
}

func (t *progressTracker) verifying(name string) {
	t.stores[name].phase.Store(int32(StoreVerifying))
}

func (t *progressTracker) finished(name string) {
	s := t.stores[name]
	s.end.Store(time.Now().UnixNano())
	s.phase.Store(int32(StoreDone))
}

func (t *progressTracker) snapshot() []StoreProgress {
	now := time.Now().UnixNano()
	out := make([]StoreProgress, 0, len(t.names))
	for _, name := range t.names {
		s := t.stores[name]
		p := StoreProgress{Name: name, Phase: StorePhase(s.phase.Load()), Total: s.total.Load(), Done: s.done.Load()}
		if start := s.start.Load(); start != 0 {
			end := s.end.Load()
			if end == 0 {
				end = now
			}
			p.Elapsed = time.Duration(end - start)
		}
		out = append(out, p)
	}
	return out
}

// report calls fn with a snapshot every interval until the returned stop
// function is called, which delivers one final snapshot.
func (t *progressTracker) report(fn func([]StoreProgress), interval time.Duration) (stop func()) {
	quit := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			fn(t.snapshot())
			select {
			case <-quit:
				return
			case <-ticker.C:
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(quit)
			wg.Wait()
			fn(t.snapshot())
		})
	}
}

// LiveProgress redraws the store table in place on a terminal. When the table
// does not fit the terminal height, finished and waiting stores collapse into
// one summary line each, so the cursor can always move back to the top.
type LiveProgress struct {
	w     io.Writer
	rows  func() int
	lines int
}

// NewLiveProgress draws to w. rows reports the terminal height; it may return
// 0 when the height is unknown, which always draws the full table.
func NewLiveProgress(w io.Writer, rows func() int) *LiveProgress {
	return &LiveProgress{w: w, rows: rows}
}

// Render replaces the previously drawn block with the given snapshot. Lines
// stay below 80 columns so they never wrap and break the redraw.
func (p *LiveProgress) Render(stores []StoreProgress) {
	lines := progressLines(stores, p.rows())

	var b bytes.Buffer
	if p.lines > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", p.lines)
	}
	for _, line := range lines {
		fmt.Fprintf(&b, "\x1b[2K%s\n", line)
	}
	// a compact block can be shorter than the previous one; clear what is left below
	b.WriteString("\x1b[J")
	p.lines = len(lines)
	_, _ = p.w.Write(b.Bytes())
}

// progressLines lays out the snapshot in at most rows-1 lines, keeping the
// last terminal row free for the cursor.
func progressLines(stores []StoreProgress, rows int) []string {
	var total, done int64
	var finished, waiting int
	var active []StoreProgress
	for _, s := range stores {
		total += s.Total
		done += s.Done
		switch s.Phase {
		case StoreDone:
			finished++
		case StoreWaiting:
			waiting++
		default:
			active = append(active, s)
		}
	}
	totalLine := fmt.Sprintf("%-16s %s  %d/%d stores", "total", bar(done, total), finished, len(stores))

	if rows <= 0 || len(stores)+1 <= rows-1 {
		lines := make([]string, 0, len(stores)+1)
		for _, s := range stores {
			lines = append(lines, formatStoreLine(s))
		}
		return append(lines, totalLine)
	}

	// compact: active stores, then the summaries and the total
	room := max(rows-1-3, 0)
	lines := make([]string, 0, rows)
	for i, s := range active {
		if i == room {
			break
		}
		lines = append(lines, formatStoreLine(s))
	}
	lines = append(lines,
		fmt.Sprintf("%-16s %d stores", "done", finished),
		fmt.Sprintf("%-16s %d stores", "waiting", waiting),
		totalLine)
	return lines
}

func formatStoreLine(s StoreProgress) string {
	name := s.Name
	if len(name) > 16 {
		name = name[:16]
	}
	switch s.Phase {
	case StoreWaiting:
		return fmt.Sprintf("%-16s waiting", name)
	case StoreDone:
		return fmt.Sprintf("%-16s %s  took %s", name, bar(s.Done, s.Total), roundDuration(s.Elapsed))
	case StoreVerifying:
		return fmt.Sprintf("%-16s %s  verifying", name, bar(s.Done, s.Total))
	}
	line := fmt.Sprintf("%-16s %s", name, bar(s.Done, s.Total))
	if s.Done > 0 && s.Elapsed > 0 {
		rate := float64(s.Done) / s.Elapsed.Seconds()
		line += fmt.Sprintf(" %7.0f/s", rate)
		if s.Total > s.Done {
			eta := time.Duration(float64(s.Total-s.Done) / rate * float64(time.Second))
			line += fmt.Sprintf("  ~%s", eta.Round(time.Second))
		}
	}
	return line
}

// roundDuration keeps durations readable at every scale, so stores that finish
// within milliseconds do not all read as 0s.
func roundDuration(d time.Duration) time.Duration {
	switch {
	case d < time.Second:
		return d.Round(time.Millisecond)
	case d < time.Minute:
		return d.Round(100 * time.Millisecond)
	}
	return d.Round(time.Second)
}

// bar renders a fixed-width progress bar with percentage and counts.
func bar(done, total int64) string {
	const width = 16
	frac := 1.0
	if total > 0 {
		frac = min(float64(done)/float64(total), 1)
	}
	filled := int(frac * width)
	return fmt.Sprintf("[%s%s] %5.1f%% %11s", strings.Repeat("#", filled), strings.Repeat(".", width-filled),
		100*frac, shortCount(done)+"/"+shortCount(total))
}

func shortCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}
