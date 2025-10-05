// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package mockbinary

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordingProbe struct {
	failures []string
}

func (*recordingProbe) Helper() {}

func (p *recordingProbe) Fatalf(format string, args ...any) {
	p.failures = append(p.failures, fmt.Sprintf(format, args...))

	panic("recording fatal")
}

func TestMock_RecordingsRejectCorruption(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, data, diagnostic string
	}{
		// A record that does not end at a NUL means the last stub was
		// killed mid-write. Reporting fewer invocations than happened is
		// the one outcome a test cannot detect for itself.
		{name: "unterminated", data: "probe\x00/dir\x000", diagnostic: "do not end at a record boundary"},
		{name: "truncated-header", data: "probe\x00", diagnostic: "truncated record header"},
		{name: "missing-name", data: "\x00/dir\x000\x00\x00", diagnostic: "missing name"},
		{name: "bad-argc", data: "probe\x00/dir\x00two\x00\x00", diagnostic: "bad argument count"},
		{name: "negative-argc", data: "probe\x00/dir\x00-1\x00\x00", diagnostic: "bad argument count"},
		// argc larger than the fields present: the arguments a test would
		// assert on are simply not there.
		{name: "argc-overruns", data: "probe\x00/dir\x009\x00one\x00", diagnostic: "claims 9 arguments"},
		{name: "missing", diagnostic: "read recordings"},
		{name: "unreadable-directory", diagnostic: "read recordings"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "invocations.bin")

			switch test.name {
			case "missing":
			case "unreadable-directory":
				require.NoError(t, os.Mkdir(path, 0o700))
			default:
				require.NoError(t, os.WriteFile(path, []byte(test.data), 0o600))
			}

			probe := &recordingProbe{}
			mock := &Mock{t: probe, recPath: path}
			require.PanicsWithValue(t, "recording fatal", func() { mock.AllNames() })
			require.PanicsWithValue(t, "recording fatal", func() { mock.Invocations("absent") })
			require.Len(t, probe.failures, 2)

			for _, failure := range probe.failures {
				require.Contains(t, failure, test.diagnostic)
			}
		})
	}
}

func TestMock_RecordingsValuesAndAbsence(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "invocations.bin")

	mock := &Mock{t: t, recPath: path}

	// An empty file is no invocations, not a corrupt one: the stub simply
	// never ran.
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	require.Equal(t, []string{}, mock.AllNames())
	require.Nil(t, mock.Invocations("absent"))

	// Values chosen so the encoding has to be doing real work: an argument
	// with a newline in it, an argument with a space, a call with none at
	// all, and stdin that ends in a newline.
	data := "zeta\x00/owned/first\x002\x00one\x00two\nwords\x00payload\n\x00" +
		"alpha\x00/owned/second\x000\x00\x00" +
		"zeta\x00/owned/third\x000\x00last\x00"
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
	require.Equal(t, []string{"alpha", "zeta"}, mock.AllNames())

	want := []Invocation{
		{Name: "zeta", Args: []string{"one", "two\nwords"}, Stdin: "payload\n", Cwd: "/owned/first"},
		{Name: "zeta", Args: []string{}, Stdin: "last", Cwd: "/owned/third"},
	}
	got := mock.Invocations("zeta")
	require.Equal(t, want, got)

	// Each call returns its own copy, so an assertion cannot corrupt what a
	// later assertion reads.
	got[0].Args[1] = "snapshot mutation"
	got[1].Name = "wrong"

	require.Equal(t, want, mock.Invocations("zeta"))

	require.Equal(t, []Invocation{{Name: "alpha", Args: []string{}, Cwd: "/owned/second"}}, mock.Invocations("alpha"))
	require.Nil(t, mock.Invocations("absent"))

	names := mock.AllNames()
	names[0] = "wrong"

	require.Equal(t, []string{"alpha", "zeta"}, mock.AllNames())
}
