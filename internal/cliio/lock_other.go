// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build !unix

package cliio

import "os"

// Advisory file locking via flock(2) is unavailable on this platform, so
// the operations are best-effort no-ops — matching the contract documented
// on WithLock. reusable-ci targets Unix CI runners; this stub only keeps
// non-Unix builds compiling.
func lockExclusive(*os.File) error { return nil }

func unlock(*os.File) error { return nil }
