// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

//nolint:cyclop // Strict startup, readiness, and signal handling stay in one process boundary.
func main() {
	authority := flag.String("authority", "", "the one HTTPS origin permitted for CONNECT")
	readyFile := flag.String("ready-file", "", "private file to create with the loopback proxy URL")

	flag.Parse()

	if flag.NArg() != 0 || *authority == "" || *readyFile == "" ||
		!filepath.IsAbs(*readyFile) || filepath.Clean(*readyFile) != *readyFile {
		fmt.Fprintln(os.Stderr, "usage: credential-proxy --authority <https-origin> --ready-file <absolute-new-file>")
		os.Exit(2)
	}

	proxy, err := livetest.StartCredentialProxy([]string{*authority})
	if err != nil {
		fmt.Fprintln(os.Stderr, "credential proxy refused its authority")
		os.Exit(2)
	}

	if err = writeReadyFile(*readyFile, proxy.URL()); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = proxy.Close(ctx)

		cancel()
		fmt.Fprintln(os.Stderr, "credential proxy could not publish readiness")
		os.Exit(2)
	}

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	select {
	case <-signalContext.Done():
		stop()
	case serveErr := <-proxy.Done():
		stop()

		if serveErr != nil {
			fmt.Fprintln(os.Stderr, "credential proxy stopped unexpectedly")
			os.Exit(1)
		}

		return
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = proxy.Close(shutdownContext)

	cancel()

	if err != nil {
		fmt.Fprintln(os.Stderr, "credential proxy shutdown was incomplete")
		os.Exit(1)
	}
}

func writeReadyFile(path, proxyURL string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // Caller supplies an absolute private job path.
	if err != nil {
		return err
	}

	if _, err = fmt.Fprintln(file, proxyURL); err != nil {
		_ = file.Close()

		return err
	}

	return file.Close()
}
