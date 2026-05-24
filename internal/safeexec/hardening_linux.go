// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

//go:build linux

package safeexec

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// HardenProcess applies the minimum set of Linux process-level
// hardening that reusable-ci needs to keep secrets in RAM:
//
//   - RLIMIT_CORE = 0: no core dump on segfault. Without this, a
//     crash while a decrypted GPG key is on the heap would write the
//     key to disk in the core file.
//   - PR_SET_DUMPABLE = 0: also disallows ptrace attaching from any
//     non-root process AND disables core dumps regardless of RLIMIT.
//     Layered defence — RLIMIT_CORE controls whether the kernel
//     creates a core file; PR_SET_DUMPABLE controls whether the
//     kernel is willing to expose this process's memory at all.
//
// Errors are ignored: this is best-effort. If the syscall is denied
// by a seccomp profile or the kernel is too old, the process still
// runs — we just lose the extra layer. Logging the failure would
// leak information about the runner config; silently degrading is
// the right trade-off for a security feature.
//
// Called once at the start of main(). Safe to call multiple times
// (both operations are idempotent on the kernel side).
func HardenProcess() {
	// RLIMIT_CORE = 0 prevents the kernel from writing core dumps for
	// this process. Soft and hard limits both zero means even root
	// can't re-enable mid-flight.
	_ = syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{Cur: 0, Max: 0})

	// PR_SET_DUMPABLE = 0 is a stronger second layer: the kernel
	// refuses ptrace attach + core-dump creation for the process,
	// regardless of RLIMIT. Defends against an attacker with non-root
	// shell access who tries to attach via gdb/strace and read
	// decrypted key material from /proc/<pid>/mem.
	_ = unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
}
