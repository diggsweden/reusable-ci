// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build unix

package toolchain

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestInstallChangelogRenderer_RefusesSelectedFIFO(t *testing.T) {
	root := changelogFixtureRoot(t)
	archive, pin := changelogArchive(t)

	in := InstallChangelogRendererInput{Backend: gitCliffBin, GitCliffVersion: "2.6.1", RunID: "42",
		BinHome: filepath.Join(root, "bin"), PathFile: filepath.Join(root, runnerPathName), RunnerTemp: filepath.Join(root, "scratch"),
		Mise: InstallMiseInput{Version: "2026.1.2", LinuxX64SHA256: pin, LinuxARM64SHA256: pin, DestDir: filepath.Join(root, "mise-bin"), BaseURL: "https://fixture.invalid/mise"}}
	if err := os.Mkdir(in.BinHome, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := syscall.Mkfifo(filepath.Join(in.BinHome, gitCliffBin), 0o600); err != nil {
		t.Fatal(err)
	}

	writeChangelogCanary(t, in.PathFile, "prior PATH\n", 0o640)

	var events []string

	blockChangelogHTTP(t, func(*http.Request) (*http.Response, error) {
		events = append(events, "HTTP")

		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(archive)), Header: make(http.Header)}, nil
	})

	runner := changelogLifecycleRunner{setBin: func(string) { events = append(events, "SetBin") },
		run: func(context.Context, []string, ...string) (string, error) {
			events = append(events, "Run")

			return "", errs.ErrUnsupported
		}}
	unchanged := changelogUnchanged(t, root)

	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err := InstallChangelogRenderer(ctx, runner, &out, in)
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "regular file or symlink") {
		t.Errorf("selected FIFO refusal = %v", err)
	}

	if len(events) != 0 || out.Len() != 0 {
		t.Errorf("selected FIFO effects before refusal: events=%v output=%q", events, out.String())
	}

	unchanged()
}
