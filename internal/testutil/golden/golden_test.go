// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package golden_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/golden"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// probeT records whether Errorf or Fatalf was called. It satisfies golden.T.
type probeT struct {
	errored bool
	logs    []string
}

func (p *probeT) Helper() {}
func (p *probeT) Errorf(format string, args ...any) {
	p.errored = true
	p.logs = append(p.logs, fmt.Sprintf(format, args...))
}
func (p *probeT) Fatalf(format string, args ...any) {
	p.errored = true
	p.logs = append(p.logs, fmt.Sprintf(format, args...))
}
func (p *probeT) Logf(format string, args ...any) {
	p.logs = append(p.logs, fmt.Sprintf(format, args...))
}

func chdirInTempDir(t *testing.T, name string, contents []byte, updateMode bool) {
	t.Helper()

	previous := flag.Lookup("update").Value.String()

	require.NoError(t, flag.Set("update", strconv.FormatBool(updateMode)))
	t.Cleanup(func() { require.NoError(t, flag.Set("update", previous)) })
	fsys := testfs.NewReal(t)

	if contents != nil {
		fsys.WriteFile(filepath.Join("testdata", "golden", name), contents)
	}

	fsys.Chdir()
}

func TestEqual_Mismatch(t *testing.T) {
	chdirInTempDir(t, "sample.txt", []byte("expected\n"), false)

	p := &probeT{}
	golden.Equal(p, "sample.txt", []byte("got\n"))

	if !p.errored {
		t.Errorf("golden.Equal did not flag a mismatch")
	}

	got, err := os.ReadFile(filepath.Join("testdata", "golden", "sample.txt"))
	require.NoError(t, err)
	require.Equal(t, "expected\n", string(got))
}

func TestEqual_MissingFile(t *testing.T) {
	chdirInTempDir(t, "", nil, false) // no golden file written

	p := &probeT{}
	golden.Equal(p, "sample.txt", []byte("anything"))

	if !p.errored {
		t.Errorf("golden.Equal did not fail on missing golden file")
	}

	_, err := os.Stat(filepath.Join("testdata", "golden", "sample.txt"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestEqual_MatchingContent(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		contents []byte
		check    func(*probeT, string)
	}{
		{
			name:     "bytes",
			filename: "sample.txt",
			contents: []byte("hello\n"),
			check: func(p *probeT, name string) {
				golden.Equal(p, name, []byte("hello\n"))
			},
		},
		{
			name:     "string",
			filename: "greeting.txt",
			contents: []byte("hi\n"),
			check: func(p *probeT, name string) {
				golden.EqualString(p, name, "hi\n")
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			chdirInTempDir(t, testCase.filename, testCase.contents, false)

			p := &probeT{}
			testCase.check(p, testCase.filename)

			if p.errored {
				t.Errorf("golden equality flagged matching content: %v", p.logs)
			}
		})
	}
}

func TestEqual_UpdateMode(t *testing.T) {
	for _, test := range []struct {
		name string
		seed []byte
	}{
		{name: "create"},
		{name: "replace", seed: []byte("stale content longer than replacement\n")},
	} {
		t.Run(test.name, func(t *testing.T) {
			name := filepath.Join("nested", "sample.txt")
			chdirInTempDir(t, name, test.seed, true)

			probe := &probeT{}
			golden.EqualString(probe, name, "new\n\n")
			require.False(t, probe.errored, "%v", probe.logs)
			require.Len(t, probe.logs, 1)
			require.Contains(t, probe.logs[0], "golden: updated")

			got, err := os.ReadFile(filepath.Join("testdata", "golden", name))
			require.NoError(t, err)
			require.Equal(t, "new\n\n", string(got))
		})
	}
}

// TestEqual_CompareModeNeverWritesTheGolden is the mode-isolation control.
//
// Every other test here checks what Equal REPORTS. None checks what it leaves
// on disk when it is not updating, and that is the property the goldens depend
// on: a compare run that also wrote the file would record the wrong output as
// the expectation, and every run afterwards — including CI, which runs without
// -update — would agree with it. The failure would be silent and permanent, and
// it is the kind of change a refactor of the update branch could introduce.
func TestEqual_CompareModeNeverWritesTheGolden(t *testing.T) {
	const (
		name     = "sample.txt"
		original = "expected\n"
	)

	chdirInTempDir(t, name, []byte(original), false)

	path := filepath.Join("testdata", "golden", name)

	before, err := os.Stat(path)
	require.NoError(t, err)

	probe := &probeT{}
	golden.EqualString(probe, name, "something else entirely\n")
	require.True(t, probe.errored, "a mismatch must be reported")

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, string(got), "compare mode rewrote the golden file")

	after, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, before.Size(), after.Size())
	require.Equal(t, before.Mode(), after.Mode())
}

// TestEqual_CompareModeDoesNotCreateAMissingGolden is the same rule for the
// absent case: a missing golden is reported so someone decides whether the
// output is right, not quietly created from whatever this run produced.
func TestEqual_CompareModeDoesNotCreateAMissingGolden(t *testing.T) {
	const name = "absent.txt"

	chdirInTempDir(t, name, nil, false)

	probe := &probeT{}
	golden.EqualString(probe, name, "produced\n")
	require.True(t, probe.errored, "a missing golden must be reported")

	_, err := os.Stat(filepath.Join("testdata", "golden", name))
	require.True(t, os.IsNotExist(err), "compare mode created the golden file: %v", err)
}

// TestEqual_RefusesNamesOutsideTestdata refuses an absolute or climbing golden
// name in both modes, so -update cannot write outside testdata/golden; the
// would-be destination stays absent.
func TestEqual_RefusesNamesOutsideTestdata(t *testing.T) {
	for _, update := range []bool{false, true} {
		for _, name := range []string{"../escaped.txt", "../../escaped.txt", filepath.Join(t.TempDir(), "absolute.txt")} {
			t.Run(fmt.Sprintf("update=%t/%s", update, filepath.Base(name)), func(t *testing.T) {
				chdirInTempDir(t, "sample.txt", []byte("expected\n"), update)

				p := &probeT{}
				golden.Equal(p, name, []byte("got\n"))

				require.True(t, p.errored, "name %q was accepted", name)

				_, err := os.Stat(filepath.Join("testdata", "golden", name))
				require.True(t, os.IsNotExist(err), "a file was written for %q", name)
			})
		}
	}
}
