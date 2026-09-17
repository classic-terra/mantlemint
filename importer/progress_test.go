package importer

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatStoreLine(t *testing.T) {
	assert.Equal(t, "bank             waiting", formatStoreLine(StoreProgress{Name: "bank"}))
	assert.Equal(t, "wasm             [####............]  25.0%  3.0M/12.0M    5000/s  ~30m0s",
		formatStoreLine(StoreProgress{Name: "wasm", Phase: StoreCopying, Total: 12_000_000, Done: 3_000_000, Elapsed: 600 * time.Second}))
	assert.Equal(t, "acc              [################] 100.0%   1.0k/1.0k  verifying",
		formatStoreLine(StoreProgress{Name: "acc", Phase: StoreVerifying, Total: 1000, Done: 1000, Elapsed: time.Second}))
	// an empty store is complete, not stuck at 0%
	assert.Equal(t, "hooks-for-ibc    [################] 100.0%         0/0  took 2ms",
		formatStoreLine(StoreProgress{Name: "hooks-for-ibc", Phase: StoreDone, Elapsed: 1789 * time.Microsecond}))
	assert.Equal(t, "staking          [################] 100.0%   1.1M/1.1M  took 1m16s",
		formatStoreLine(StoreProgress{Name: "staking", Phase: StoreDone, Total: 1144841, Done: 1144841, Elapsed: 75557 * time.Millisecond}))
}

func TestRoundDuration(t *testing.T) {
	assert.Equal(t, 35*time.Millisecond, roundDuration(34567*time.Microsecond))
	assert.Equal(t, 1400*time.Millisecond, roundDuration(1437*time.Millisecond))
	assert.Equal(t, 2*time.Minute+5*time.Second, roundDuration(125432*time.Millisecond))
}

func TestLiveProgressRedrawsInPlace(t *testing.T) {
	var out bytes.Buffer
	p := NewLiveProgress(&out, func() int { return 0 })
	stores := []StoreProgress{{Name: "a", Total: 3}, {Name: "b", Phase: StoreCopying, Total: 10, Done: 5, Elapsed: time.Second}}

	p.Render(stores)
	first := out.String()
	assert.False(t, strings.Contains(first, "\x1b[3A"), "nothing to overwrite on the first draw")
	assert.Equal(t, 3, strings.Count(first, "\n"))
	assert.Contains(t, first, "total            [######..........]  38.5%        5/13  0/2 stores")

	out.Reset()
	p.Render(stores)
	assert.True(t, strings.HasPrefix(out.String(), "\x1b[3A"), "moves up over the previous block")
	for _, line := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") {
		assert.Less(t, len(line), 80+len("\x1b[3A\x1b[2K"))
	}
}

func TestProgressLinesCompactWhenTerminalIsShort(t *testing.T) {
	var stores []StoreProgress
	for i := 0; i < 10; i++ {
		stores = append(stores, StoreProgress{Name: fmt.Sprintf("done%d", i), Phase: StoreDone, Total: 1, Done: 1})
	}
	stores = append(stores,
		StoreProgress{Name: "bank", Phase: StoreCopying, Total: 10, Done: 5, Elapsed: time.Second},
		StoreProgress{Name: "wasm", Phase: StoreVerifying, Total: 10, Done: 10, Elapsed: time.Second},
		StoreProgress{Name: "staking", Total: 10},
	)

	// 13 stores and the total need 14 lines plus one free row
	assert.Len(t, progressLines(stores, 15), 14)

	lines := progressLines(stores, 14)
	assert.Len(t, lines, 5)
	assert.True(t, strings.HasPrefix(lines[0], "bank "))
	assert.True(t, strings.HasPrefix(lines[1], "wasm "))
	assert.Equal(t, "done             10 stores", lines[2])
	assert.Equal(t, "waiting          1 stores", lines[3])
	assert.True(t, strings.HasPrefix(lines[4], "total "))

	// too short even for every active store: keep the summaries and total
	lines = progressLines(stores, 5)
	assert.Len(t, lines, 4)
	assert.True(t, strings.HasPrefix(lines[0], "bank "))
}
