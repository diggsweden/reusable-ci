// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build linux

package safeexec

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// HardenProcess sets two Linux process attributes that keep in-memory secrets
// out of core files and away from unprivileged debuggers. It sets flags; it
// does not prove resistance to any attacker, and it does not cover everything
// the process starts.
//
//   - RLIMIT_CORE = 0 (soft and hard): the kernel writes no core file for
//     this process. Limits are inherited across fork and exec, so child
//     processes start with the same limit, but a process holding
//     CAP_SYS_RESOURCE (root, typically) can raise the hard limit again.
//   - PR_SET_DUMPABLE = 0: no core dump, and a same-user process without
//     CAP_SYS_PTRACE cannot ptrace this process or read /proc/<pid>/mem. It
//     does not stop root or CAP_SYS_PTRACE, and execve resets the flag, so
//     programs this process runs (gpg, cosign, git) are dumpable again unless
//     they set it themselves.
//
// Errors are ignored: this is best-effort. If a seccomp profile or the kernel
// refuses either call, the process runs without that layer and says nothing,
// since the failure would describe the runner's configuration in the log.
//
// Called once at the start of main(). Safe to call more than once; both
// settings are idempotent.
func HardenProcess() {
	_ = syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{Cur: 0, Max: 0})
	_ = unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
}
