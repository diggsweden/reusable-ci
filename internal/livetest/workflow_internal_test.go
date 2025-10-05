// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRequireRunnerABI_AcceptsLinuxAMD64ELFAndRejectsOtherContent(t *testing.T) {
	staticELF := writeSyntheticRunnerELF(t, 62, "", false)
	if err := requireRunnerABI(staticELF); err != nil {
		t.Fatalf("static linux/amd64 ELF rejected: %v", err)
	}

	interpretedELF := writeSyntheticRunnerELF(t, 62, "/lib64/ld-linux-x86-64.so.2", false)
	if err := requireRunnerABI(interpretedELF); err == nil || !strings.Contains(err.Error(), "requires an ELF interpreter") {
		t.Fatalf("interpreted runner result = %v", err)
	}

	dynamicELF := writeSyntheticRunnerELF(t, 62, "", true)
	if err := requireRunnerABI(dynamicELF); err == nil || !strings.Contains(err.Error(), "requires shared libraries") {
		t.Fatalf("runner with DT_NEEDED result = %v", err)
	}

	wrongArchitecture := writeSyntheticRunnerELF(t, 183, "", false)
	if err := requireRunnerABI(wrongArchitecture); err == nil || !strings.Contains(err.Error(), "want linux/amd64") {
		t.Fatalf("non-amd64 runner result = %v", err)
	}

	notELF := t.TempDir() + "/reusable-ci"
	if err := os.WriteFile(notELF, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // Executable text is the negative ABI fixture.
		t.Fatal(err)
	}

	if err := requireRunnerABI(notELF); err == nil || !strings.Contains(err.Error(), "not an ELF binary") {
		t.Fatalf("non-ELF runner result = %v", err)
	}
}

func TestRequireRunnerABI_DeclaredOSABIPolicy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		osabi      elf.OSABI
		abiVersion byte
		elfType    elf.Type
		accepted   bool
	}{
		{name: "sysv_exec", osabi: elf.ELFOSABI_NONE, elfType: elf.ET_EXEC, accepted: true},
		{name: "linux_exec", osabi: elf.ELFOSABI_LINUX, elfType: elf.ET_EXEC, accepted: true},
		{name: "sysv_dyn", osabi: elf.ELFOSABI_NONE, elfType: elf.ET_DYN, accepted: true},
		{name: "linux_dyn", osabi: elf.ELFOSABI_LINUX, elfType: elf.ET_DYN, accepted: true},
		{name: "freebsd", osabi: elf.ELFOSABI_FREEBSD, elfType: elf.ET_EXEC},
		{name: "unknown", osabi: elf.OSABI(255), elfType: elf.ET_EXEC},
		{name: "sysv_version", osabi: elf.ELFOSABI_NONE, abiVersion: 1, elfType: elf.ET_EXEC},
		{name: "linux_version", osabi: elf.ELFOSABI_LINUX, abiVersion: 1, elfType: elf.ET_EXEC},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSyntheticRunnerELF(t, 62, "", false)
			body, err := os.ReadFile(path)
			require.NoError(t, err)

			body[7], body[8] = byte(tc.osabi), tc.abiVersion
			binary.LittleEndian.PutUint16(body[16:], uint16(tc.elfType)) //nolint:gosec // Fixture uses only ET_EXEC/ET_DYN.
			require.NoError(t, os.WriteFile(path, body, 0o600))          //nolint:gosec // Owned synthetic ELF is inspected, never executed.

			err = requireRunnerABI(path)
			if tc.accepted {
				require.NoError(t, err)

				return
			}

			if !errors.Is(err, errRunnerABI) || !strings.Contains(err.Error(), "declares OSABI") {
				t.Fatalf("got %v, want declared OSABI refusal", err)
			}
		})
	}
}

// TestRequireRunnerABI_HeaderSegmentAndDynamicTableRefusals patches a static
// linux/amd64 fixture one field at a time and pins each outcome without running
// anything: a 32-bit class, big-endian data, relocatable and core object types,
// no loadable segment, and dynamic tables that are empty, misaligned, oversized
// or unterminated are refused as runner ABI errors; a dynamic table holding
// only a terminator, with no needed libraries, is accepted.
func TestRequireRunnerABI_HeaderSegmentAndDynamicTableRefusals(t *testing.T) {
	t.Parallel()

	const (
		headerSize    = 64
		programSize   = 56
		dynamicHeader = headerSize + programSize
	)

	for _, tc := range []struct {
		name    string
		dynamic bool
		patch   func(body []byte)
		want    string
	}{
		{name: "static", patch: func([]byte) {}},
		{name: "class_32", patch: func(b []byte) { b[4] = byte(elf.ELFCLASS32) }, want: ""},
		{name: "big_endian", patch: func(b []byte) { b[5] = byte(elf.ELFDATA2MSB) }, want: ""},
		{name: "relocatable", patch: func(b []byte) { binary.LittleEndian.PutUint16(b[16:], uint16(elf.ET_REL)) }, want: "not an executable ELF"},
		{name: "core", patch: func(b []byte) { binary.LittleEndian.PutUint16(b[16:], uint16(elf.ET_CORE)) }, want: "not an executable ELF"},
		{name: "no_load_segment", patch: func(b []byte) { binary.LittleEndian.PutUint32(b[headerSize:], uint32(elf.PT_NOTE)) }, want: "no loadable segment"},
		{name: "dynamic_terminator_only", dynamic: true, patch: func(b []byte) {
			offset := binary.LittleEndian.Uint64(b[dynamicHeader+8:])
			binary.LittleEndian.PutUint64(b[offset:], uint64(elf.DT_NULL))
		}},
		{name: "dynamic_empty", dynamic: true, patch: func(b []byte) { binary.LittleEndian.PutUint64(b[dynamicHeader+32:], 0) }, want: "inspect runner asset"},
		{name: "dynamic_misaligned", dynamic: true, patch: func(b []byte) { binary.LittleEndian.PutUint64(b[dynamicHeader+32:], 24) }, want: "inspect runner asset"},
		{name: "dynamic_oversized", dynamic: true, patch: func(b []byte) { binary.LittleEndian.PutUint64(b[dynamicHeader+32:], 2<<20) }, want: "inspect runner asset"},
		{name: "dynamic_unterminated", dynamic: true, patch: func(b []byte) {
			offset := binary.LittleEndian.Uint64(b[dynamicHeader+8:])
			binary.LittleEndian.PutUint64(b[offset:], uint64(elf.DT_STRSZ))
			binary.LittleEndian.PutUint64(b[offset+16:], uint64(elf.DT_STRSZ))
		}, want: "inspect runner asset"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeSyntheticRunnerELF(t, 62, "", tc.dynamic)
			body, err := os.ReadFile(path)
			require.NoError(t, err)

			tc.patch(body)
			require.NoError(t, os.WriteFile(path, body, 0o600)) //nolint:gosec // Owned synthetic ELF is inspected, never executed.

			err = requireRunnerABI(path)

			switch {
			case tc.name == "big_endian" || tc.name == "class_32":
				// debug/elf reads the rest of the header in the declared class
				// and byte order, so a flipped one no longer parses as this
				// layout; it is refused either way and never accepted.
				require.Error(t, err)
			case tc.want == "":
				require.NoError(t, err)
			default:
				require.ErrorIs(t, err, errRunnerABI)
				require.Contains(t, err.Error(), tc.want)
			}
		})
	}
}

func writeSyntheticRunnerELF(t *testing.T, machine uint16, interpreter string, dynamic bool) string {
	t.Helper()

	programCount := uint16(1)
	if interpreter != "" {
		programCount++
	}

	if dynamic {
		programCount++
	}

	const (
		headerSize  = 64
		programSize = 56
	)

	payloadOffset := headerSize + programSize*int(programCount)

	bodySize := payloadOffset
	if interpreter != "" {
		bodySize += len(interpreter) + 1
	}

	if dynamic {
		bodySize += 32
	}

	body := make([]byte, bodySize)
	copy(body, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(body[16:], 2)       // ET_EXEC
	binary.LittleEndian.PutUint16(body[18:], machine) // e_machine
	binary.LittleEndian.PutUint32(body[20:], 1)       // EV_CURRENT
	binary.LittleEndian.PutUint64(body[32:], headerSize)
	binary.LittleEndian.PutUint16(body[52:], headerSize)
	binary.LittleEndian.PutUint16(body[54:], programSize)
	binary.LittleEndian.PutUint16(body[56:], programCount)

	binary.LittleEndian.PutUint32(body[headerSize:], 1)   // PT_LOAD
	binary.LittleEndian.PutUint32(body[headerSize+4:], 5) // PF_R | PF_X
	binary.LittleEndian.PutUint64(body[headerSize+32:], uint64(len(body)))
	binary.LittleEndian.PutUint64(body[headerSize+40:], uint64(len(body)))

	nextProgram := headerSize + programSize

	if interpreter != "" {
		copy(body[payloadOffset:], interpreter)

		program := nextProgram
		binary.LittleEndian.PutUint32(body[program:], 3) // PT_INTERP
		binary.LittleEndian.PutUint64(body[program+8:], uint64(payloadOffset))
		binary.LittleEndian.PutUint64(body[program+32:], uint64(len(interpreter)+1))
		binary.LittleEndian.PutUint64(body[program+40:], uint64(len(interpreter)+1))

		nextProgram += programSize
		payloadOffset += len(interpreter) + 1
	}

	if dynamic {
		binary.LittleEndian.PutUint32(body[nextProgram:], 2) // PT_DYNAMIC
		binary.LittleEndian.PutUint64(body[nextProgram+8:], uint64(payloadOffset))
		binary.LittleEndian.PutUint64(body[nextProgram+32:], 32)
		binary.LittleEndian.PutUint64(body[nextProgram+40:], 32)
		binary.LittleEndian.PutUint64(body[payloadOffset:], 1) // DT_NEEDED
		binary.LittleEndian.PutUint64(body[payloadOffset+8:], 1)
	}

	path := filepath.Join(t.TempDir(), "runner")
	if err := os.WriteFile(path, body, 0o700); err != nil { //nolint:gosec // Synthetic ELF fixture must be executable.
		t.Fatal(err)
	}

	return path
}

func TestProbePrelude_VerifiesRunnerDigestAndVersionBeforeInvocation(t *testing.T) {
	digest := strings.Repeat("a", 64)
	t.Setenv(runnerBinarySHA256Env, digest)

	caFile := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caFile, independentCAPEM(t), 0o600); err != nil {
		t.Fatal(err)
	}

	target := withCAFacts(t, Target{CAFile: caFile, accepted: true})

	script := ProbePrelude(target, "https://fixture.invalid/reusable-ci")
	digestCheck := "printf '%s  reusable-ci\\n' '" + digest + "' | sha256sum --check --strict"
	digestAt := strings.Index(script, digestCheck)
	versionAt := strings.Index(script, "./reusable-ci --version")

	runAt := strings.Index(script, "run_product()")
	if digestAt < 0 || versionAt < digestAt || runAt < versionAt {
		t.Fatalf("runner verification order is digest=%d version=%d invocation=%d", digestAt, versionAt, runAt)
	}
}

func TestProbePrelude_RejectsMalformedRunnerDigest(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(caFile, independentCAPEM(t), 0o600))
	target := withCAFacts(t, Target{CAFile: caFile, accepted: true})
	t.Setenv(runnerBinarySHA256Env, strings.Repeat("a", 64))
	require.NotEmpty(t, ProbePrelude(target, "https://fixture.invalid/reusable-ci"))

	for _, digest := range []string{"", "ABC", strings.Repeat("A", 64), strings.Repeat("g", 64), strings.Repeat("a", 63), strings.Repeat("a", 65)} {
		t.Run("digest="+digest, func(t *testing.T) {
			t.Setenv(runnerBinarySHA256Env, digest)
			defer func() {
				if recovered := recover(); recovered != runnerBinarySHA256Env+" must be a lowercase SHA-256" {
					t.Errorf("wrong or missing digest refusal: %v", recovered)
				}
			}()

			_ = ProbePrelude(target, "https://fixture.invalid/reusable-ci")

			t.Error("invalid digest returned a rendered prelude")
		})
	}
}

func TestProbePrelude_ChecksBytesBeforeExecutingFixture(t *testing.T) {
	const binary = "#!/bin/sh\nprintf '%s\\n' \"$*\" >> invoked\nprintf 'artifacts.yml present\\n'\n"

	for _, match := range []bool{true, false} {
		t.Run(fmt.Sprintf("matching=%t", match), func(t *testing.T) {
			root := t.TempDir()
			tools := filepath.Join(root, "tools")
			require.NoError(t, os.Mkdir(tools, 0o700))

			fixture := filepath.Join(root, "fixture-binary")
			require.NoError(t, os.WriteFile(fixture, []byte(binary), 0o600))
			// The only fetch command is an owned copy stub. CA output is also
			// redirected into this fixture tree; no shared /tmp or host trust is changed.
			require.NoError(t, os.WriteFile(filepath.Join(tools, "curl"), []byte("#!/bin/sh\n[ \"$1\" = -fsSL ] && [ \"$2\" = -o ] && [ \"$3\" = reusable-ci ] && [ \"$4\" = https://fixture.invalid/reusable-ci ] || exit 90\ncp \"$FIXTURE_BINARY\" reusable-ci\n"), 0o700)) //nolint:gosec // executable fixture, not a downloaded tool.
			caFile := filepath.Join(root, "source-ca.crt")
			require.NoError(t, os.WriteFile(caFile, independentCAPEM(t), 0o600))
			target := withCAFacts(t, Target{CAFile: caFile, accepted: true})

			digest := fmt.Sprintf("%x", sha256.Sum256([]byte(binary)))
			if !match {
				digest = strings.Repeat("0", 64)
			}

			t.Setenv(runnerBinarySHA256Env, digest)

			script := strings.ReplaceAll(ProbePrelude(target, "https://fixture.invalid/reusable-ci"), LabCAPath, filepath.Join(root, "lab-ca.crt")) + "\nrun_product doctor\n"

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, "/bin/bash", "--noprofile", "--norc", "-eu", "-c", script) //nolint:gosec // bounded fixture script, owned curl substitute and closed environment.
			cmd.Dir = root
			cmd.Env = []string{"PATH=" + tools + ":/usr/bin:/bin", "HOME=" + root, "TMPDIR=" + root, "FIXTURE_BINARY=" + fixture}
			out, err := cmd.CombinedOutput()

			require.NoError(t, ctx.Err())

			invoked, readErr := os.ReadFile(filepath.Join(root, "invoked"))

			if match {
				require.NoError(t, err, "%s", out)
				require.NoError(t, readErr)
				require.Equal(t, "--version\ndoctor\n", string(invoked))
			} else {
				require.Error(t, err)
				require.ErrorIs(t, readErr, os.ErrNotExist)
			}
		})
	}
}
