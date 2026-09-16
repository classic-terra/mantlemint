package importer

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/terra-money/mantlemint/db/heleveldb"
)

func TestMainImports(t *testing.T) {
	source := newSourceHome(t, 3)
	target := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := Main([]string{"-app-home", source, "-mantlemint-home", target, "-workers", "2"}, &stdout, &stderr)
	assert.Equal(t, 0, code, stderr.String())
	assert.Contains(t, stdout.String(), "imported chain rehearsal-1 at height 3")
	assert.Contains(t, stdout.String(), "CHAIN_ID=rehearsal-1")

	db := openImported(t, target)
	state, err := db.driver.ImportState()
	assert.Nil(t, err)
	assert.Equal(t, heleveldb.ImportStateComplete, state)
}

func TestMainRequiresHomes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	assert.Equal(t, 2, Main([]string{"-app-home", t.TempDir()}, &stdout, &stderr))
	assert.Contains(t, stderr.String(), "-app-home and -mantlemint-home are required")
	assert.Empty(t, stdout.String())
}

func TestMainRejectsUnknownFlagsAndArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	assert.Equal(t, 2, Main([]string{"-no-such-flag"}, &stdout, &stderr))
	assert.Equal(t, 2, Main([]string{"-app-home", "a", "-mantlemint-home", "b", "extra"}, &stdout, &stderr))
	assert.Contains(t, stderr.String(), "unexpected arguments: [extra]")
}

func TestMainHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	assert.Equal(t, 0, Main([]string{"-h"}, &stdout, &stderr))
	assert.Contains(t, stderr.String(), "Usage: mantlemint import")
}

func TestMainReportsFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Main([]string{"-app-home", t.TempDir(), "-mantlemint-home", t.TempDir()}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "[import]")
}
