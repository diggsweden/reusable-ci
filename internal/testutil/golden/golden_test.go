// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package golden_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/testutil/golden"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
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

func chdirInTempDir(t *testing.T, name string, contents []byte) {
	t.Helper()
	fsys := testfs.NewReal(t)

	if contents != nil {
		fsys.WriteFile(filepath.Join("testdata", "golden", name), contents)
	}

	fsys.Chdir()
}

func TestEqual_Mismatch(t *testing.T) {
	chdirInTempDir(t, "sample.txt", []byte("expected\n"))

	p := &probeT{}
	golden.Equal(p, "sample.txt", []byte("got\n"))

	if !p.errored {
		t.Errorf("golden.Equal did not flag a mismatch")
	}
}

func TestEqual_MissingFile(t *testing.T) {
	chdirInTempDir(t, "", nil) // no golden file written

	p := &probeT{}
	golden.Equal(p, "sample.txt", []byte("anything"))

	if !p.errored {
		t.Errorf("golden.Equal did not fail on missing golden file")
	}
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
			chdirInTempDir(t, testCase.filename, testCase.contents)

			p := &probeT{}
			testCase.check(p, testCase.filename)

			if p.errored {
				t.Errorf("golden equality flagged matching content: %v", p.logs)
			}
		})
	}
}
