// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

//go:build !linux

package safeexec

// HardenProcess is a no-op on non-Linux platforms. The Linux
// implementation suppresses core dumps and ptrace via RLIMIT_CORE +
// PR_SET_DUMPABLE; equivalent knobs exist on macOS (ulimit -c) and
// Windows (CrashControl/AeDebug) but production CI for diggsweden
// runs on Linux runners, so we ship the Linux hardening and leave
// other platforms behavioural-no-op.
//
// macOS support could be added later via setrlimit(2) (POSIX) and
// PT_DENY_ATTACH; not in scope today.
func HardenProcess() {}
