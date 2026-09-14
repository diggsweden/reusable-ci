// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func writeCAFixture(t *testing.T, path string, body []byte, mode os.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestReadCAFile_AcceptsOwnerControlledPublicReadableCertificate(t *testing.T) {
	t.Parallel()

	body := independentCAPEM(t)
	path := filepath.Join(t.TempDir(), "ca.pem")
	writeCAFixture(t, path, body, 0o644)

	got, err := readCAFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, body) {
		t.Fatal("frozen CA differs from the bytes read through the bound descriptor")
	}
}

func TestReadCAFile_RejectsWritableOrSymlinkedSource(t *testing.T) {
	t.Parallel()

	body := independentCAPEM(t)
	t.Run("group or world writable", func(t *testing.T) {
		t.Helper()

		path := filepath.Join(t.TempDir(), "ca.pem")
		writeCAFixture(t, path, body, 0o666)

		if _, err := readCAFile(path, nil); err == nil || !strings.Contains(err.Error(), "writable by group or other") {
			t.Fatalf("writable CA result = %v", err)
		}
	})
	t.Run("symlink parent", func(t *testing.T) {
		t.Helper()

		root := t.TempDir()

		realParent := filepath.Join(root, "real")
		if err := os.Mkdir(realParent, 0o700); err != nil {
			t.Fatal(err)
		}

		writeCAFixture(t, filepath.Join(realParent, "ca.pem"), body, 0o644)

		linkParent := filepath.Join(root, "linked")
		if err := os.Symlink(realParent, linkParent); err != nil {
			t.Fatal(err)
		}

		if _, err := readCAFile(filepath.Join(linkParent, "ca.pem"), nil); err == nil || !strings.Contains(err.Error(), "symbolic links") {
			t.Fatalf("symlink-parent CA result = %v", err)
		}
	})
}

func TestReadCAFile_RejectsDeterministicReplacementAndMutation(t *testing.T) {
	t.Parallel()

	t.Run("replacement after inspection", func(t *testing.T) {
		t.Helper()

		path := filepath.Join(t.TempDir(), "ca.pem")
		writeCAFixture(t, path, independentCAPEM(t), 0o600)

		hook := func(phase caReadPhase) {
			if phase != caSourceInspected {
				return
			}

			if err := os.Rename(path, path+".old"); err != nil {
				t.Fatal(err)
			}

			writeCAFixture(t, path, independentCAPEM(t), 0o600)
		}

		if _, err := readCAFile(path, hook); err == nil || !strings.Contains(err.Error(), "changed while it was opened") {
			t.Fatalf("replaced CA result = %v", err)
		}
	})
	t.Run("in-place mutation after read", func(t *testing.T) {
		t.Helper()

		path := filepath.Join(t.TempDir(), "ca.pem")
		body := independentCAPEM(t)
		writeCAFixture(t, path, body, 0o600)

		hook := func(phase caReadPhase) {
			if phase == caSourceRead {
				writeCAFixture(t, path, append(body, '\n'), 0o600)
			}
		}

		if _, err := readCAFile(path, hook); err == nil || !strings.Contains(err.Error(), "changed while it was read") {
			t.Fatalf("mutated CA result = %v", err)
		}
	})
}

func TestVerifyCleanupBindings_UsesCapturedIdentityAndDigest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	command := filepath.Join(dir, "cleanup")
	contractFile := filepath.Join(dir, "recovery.env")

	if err := os.WriteFile(command, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil { //nolint:gosec // Executable fixture must be owner-executable.
		t.Fatal(err)
	}

	if err := os.WriteFile(contractFile, []byte("recovery\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	commandBinding, contractBinding, err := bindCleanupFiles(command, nil, contractFile)
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifyCleanupBindings(command, commandBinding.Facts, contractFile, contractBinding.Facts); err != nil {
		t.Fatalf("unchanged cleanup bindings rejected: %v", err)
	}

	if err := os.WriteFile(contractFile, []byte("mutated\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := VerifyCleanupBindings(command, commandBinding.Facts, contractFile, contractBinding.Facts); err == nil ||
		!strings.Contains(err.Error(), "cleanup contract_file changed") {
		t.Fatalf("mutated cleanup binding result = %v", err)
	}
}

func TestBindCleanupFiles_VerifiesProducerCommandDigest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	command := filepath.Join(dir, "cleanup")
	contractFile := filepath.Join(dir, "recovery.env")
	commandBody := []byte("#!/bin/sh\nexit 0\n")

	if err := os.WriteFile(command, commandBody, 0o700); err != nil { //nolint:gosec // Executable fixture must be owner-executable.
		t.Fatal(err)
	}

	if err := os.WriteFile(contractFile, []byte("recovery\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	digest := hexDigest(commandBody)
	if _, _, err := bindCleanupFiles(command, &digest, contractFile); err != nil {
		t.Fatalf("matching producer digest rejected: %v", err)
	}

	wrongDigest := strings.Repeat("a", sha256.Size*2)
	if wrongDigest == digest {
		wrongDigest = strings.Repeat("b", sha256.Size*2)
	}

	if _, _, err := bindCleanupFiles(command, &wrongDigest, contractFile); err == nil ||
		!strings.Contains(err.Error(), "does not match command_sha256") {
		t.Fatalf("mismatched producer digest result = %v", err)
	}
}

func TestValidateLiveProfile_FullRequiresCompleteSingleRoad(t *testing.T) {
	issuer := func(endpoint string) labFulcioIssuer {
		return labFulcioIssuer{Endpoint: endpoint, OIDCIssuer: "https://" + endpoint + ".compose.forgelab:8443"}
	}
	endpoint := func(name, kind, road string) labEndpoint {
		return labEndpoint{
			Name: name, Kind: kind,
			WebBaseURL:   "https://" + name + "." + road + ".forgelab:8443",
			Capabilities: []string{"workflow-runs"},
		}
	}

	base := labContract{
		Endpoints: []labEndpoint{
			endpoint("gitlab", string(provider.ForgeGitLab), "compose"),
			endpoint("forgejo", string(provider.ForgeForgejo), "compose"),
		},
		Fulcio: requiredNullable[labFulcio]{Set: true, Value: &labFulcio{
			Issuers: []labFulcioIssuer{issuer("gitlab"), issuer("forgejo")},
		}},
	}

	t.Setenv(liveProfileEnv, "full")
	t.Setenv(labRunnerForgesEnv, "gitlab,forgejo")
	t.Setenv(liveExpectedRoadEnv, "compose")

	if err := validateLiveProfile(base, 2); err != nil {
		t.Fatalf("complete full profile rejected: %v", err)
	}

	t.Run("both endpoints", func(t *testing.T) {
		t.Helper()

		if err := validateLiveProfile(base, 1); err == nil || !strings.Contains(err.Error(), "requires both") {
			t.Fatalf("single-endpoint result = %v", err)
		}
	})
	t.Run("explicit road", func(t *testing.T) {
		t.Helper()

		t.Setenv(liveExpectedRoadEnv, "")

		if err := validateLiveProfile(base, 2); err == nil || !strings.Contains(err.Error(), liveExpectedRoadEnv) {
			t.Fatalf("missing-road result = %v", err)
		}
	})
	t.Run("both runners", func(t *testing.T) {
		t.Helper()

		t.Setenv(labRunnerForgesEnv, "gitlab")

		if err := validateLiveProfile(base, 2); err == nil || !strings.Contains(err.Error(), labRunnerForgesEnv) {
			t.Fatalf("missing-runner result = %v", err)
		}
	})
	t.Run("workflow capability", func(t *testing.T) {
		t.Helper()

		lab := base
		lab.Endpoints = append([]labEndpoint(nil), base.Endpoints...)

		lab.Endpoints[1].Capabilities = nil
		if err := validateLiveProfile(lab, 2); err == nil || !strings.Contains(err.Error(), "workflow-runs") {
			t.Fatalf("missing-capability result = %v", err)
		}
	})
	t.Run("fulcio mapping", func(t *testing.T) {
		t.Helper()

		lab := base
		fulcio := *base.Fulcio.Value
		fulcio.Issuers = fulcio.Issuers[:1]

		lab.Fulcio.Value = &fulcio
		if err := validateLiveProfile(lab, 2); err == nil || !strings.Contains(err.Error(), "Fulcio") {
			t.Fatalf("missing-fulcio result = %v", err)
		}
	})
	t.Run("single road", func(t *testing.T) {
		t.Helper()

		lab := base
		lab.Endpoints = append([]labEndpoint(nil), base.Endpoints...)

		lab.Endpoints[1] = endpoint("forgejo", string(provider.ForgeForgejo), "k3s")
		if err := validateLiveProfile(lab, 2); err == nil || !strings.Contains(err.Error(), "span roads") {
			t.Fatalf("mixed-road result = %v", err)
		}
	})
	t.Run("requested road", func(t *testing.T) {
		t.Helper()

		t.Setenv(liveExpectedRoadEnv, "k3s")

		if err := validateLiveProfile(base, 2); err == nil || !strings.Contains(err.Error(), "expected k3s") {
			t.Fatalf("wrong-road result = %v", err)
		}
	})
}

// TestRunFrozenCleanup_LaunchContract drives RunFrozenCleanup with a
// self-owned launcher that only records what it was given. It is not
// parallel: it writes executables and sets the environment, and a concurrent
// fork holding a write descriptor would make the exec fail with ETXTBSY.
func TestRunFrozenCleanup_LaunchContract(t *testing.T) { //nolint:gocognit // One subtest per launch outcome, sharing the recording launcher.
	const marker = "RC_FROZEN_CLEANUP_TEST_MARKER"

	t.Setenv(marker, "inherited")

	type fixture struct {
		command, contractFile, record string
		commandFacts, contractFacts   string
	}

	arrange := func(t *testing.T, launcher string) fixture {
		t.Helper()

		dir := t.TempDir()
		f := fixture{
			command:      filepath.Join(dir, "cleanup"),
			contractFile: filepath.Join(dir, "recovery.env"),
			record:       filepath.Join(dir, "record"),
		}

		script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$#\" \"$1\" \"${%s-unset}\" >%q\n%s\n", marker, f.record, launcher)
		if err := os.WriteFile(f.command, []byte(script), 0o700); err != nil { //nolint:gosec // The launcher fixture must be owner-executable.
			t.Fatal(err)
		}

		if err := os.WriteFile(f.contractFile, []byte("recovery\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		commandBinding, contractBinding, err := bindCleanupFiles(f.command, nil, f.contractFile)
		if err != nil {
			t.Fatal(err)
		}

		f.commandFacts, f.contractFacts = commandBinding.Facts, contractBinding.Facts

		return f
	}

	run := func(ctx context.Context, f fixture) error {
		return RunFrozenCleanup(ctx, f.command, f.commandFacts, f.contractFile, f.contractFacts)
	}

	t.Run("launches the pinned launcher with only the recovery path and the inherited environment", func(t *testing.T) {
		t.Helper()

		f := arrange(t, "exit 0")
		if err := run(t.Context(), f); err != nil {
			t.Fatal(err)
		}

		if got, want := readRecord(t, f.record), "1\n"+f.contractFile+"\ninherited\n"; got != want {
			t.Fatalf("launcher received %q, want %q", got, want)
		}
	})

	t.Run("a failing launcher is a launched failure with its status", func(t *testing.T) {
		t.Helper()

		f := arrange(t, "exit 7")

		err := run(t.Context(), f)

		var exit *exec.ExitError
		if !errors.Is(err, ErrFrozenCleanupFailed) || !errors.As(err, &exit) || exit.ExitCode() != 7 {
			t.Fatalf("failing launcher = %v, want ErrFrozenCleanupFailed with exit 7", err)
		}
	})

	t.Run("a hung launcher is stopped at the context bound", func(t *testing.T) {
		t.Helper()

		f := arrange(t, "exec sleep 30")

		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()

		started := time.Now()
		err := run(ctx, f)

		if !errors.Is(err, ErrFrozenCleanupFailed) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("hung launcher = %v, want ErrFrozenCleanupFailed with the deadline", err)
		}

		if elapsed := time.Since(started); elapsed > 10*time.Second {
			t.Fatalf("hung launcher returned after %s, want the SIGTERM path well inside the grace period", elapsed)
		}
	})

	t.Run("a replaced launcher or changed recovery file is refused before launch", func(t *testing.T) {
		t.Helper()

		for name, change := range map[string]func(fixture) error{
			"launcher replaced": func(f fixture) error {
				replacement := f.command + ".new"
				if err := os.WriteFile(replacement, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil { //nolint:gosec // Owner-executable replacement fixture.
					return err
				}

				return os.Rename(replacement, f.command)
			},
			"recovery file changed": func(f fixture) error {
				return os.WriteFile(f.contractFile, []byte("changed\n"), 0o600)
			},
		} {
			f := arrange(t, "exit 0")
			if err := change(f); err != nil {
				t.Fatal(err)
			}

			err := run(t.Context(), f)
			if err == nil || errors.Is(err, ErrFrozenCleanupFailed) || !strings.Contains(err.Error(), "changed during the live run") {
				t.Errorf("%s: %v, want a refusal before launch", name, err)
			}

			if _, statErr := os.Stat(f.record); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("%s: the launcher ran: %v", name, statErr)
			}
		}
	})
}

func readRecord(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // Owned test record.
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}

// boundFileRole is one file whose identity preflight freezes: how it is bound,
// how a later use checks it against the captured facts, and the refusal that
// check gives.
type boundFileRole struct {
	name          string
	mode, altMode os.FileMode
	body          func(*testing.T) []byte
	bind          func(path string) (facts string, body []byte, err error)
	verify        func(path, facts string) error
	changed       string
}

func boundFileRoles() []boundFileRole {
	owned := func(executable bool) func(string) (string, []byte, error) {
		return func(path string) (string, []byte, error) {
			binding, err := bindOwnedFile(path, executable)

			return binding.Facts, binding.Body, err
		}
	}
	text := func(body string) func(*testing.T) []byte { return func(*testing.T) []byte { return []byte(body) } }

	return []boundFileRole{
		{
			name: "ca_file", mode: 0o400, altMode: 0o444, body: independentCAPEM,
			bind: func(path string) (string, []byte, error) {
				body, facts, err := readCAFileBound(path, nil)

				return facts, body, err
			},
			verify:  func(path, facts string) error { return verifyTargetCAPath(Target{CAFile: path, CAFacts: facts}) },
			changed: "frozen ca_file changed after preflight",
		},
		{
			name: "cleanup command", mode: 0o700, altMode: 0o500, body: text("#!/bin/sh\nexit 0\n"), bind: owned(true),
			verify:  func(path, facts string) error { return verifyCleanupBinding("cleanup command", path, facts, true) },
			changed: "validated cleanup command changed during the live run",
		},
		{
			name: "cleanup contract_file", mode: 0o600, altMode: 0o400, body: text("recovery=1\n"), bind: owned(false),
			verify: func(path, facts string) error {
				return verifyCleanupBinding("cleanup contract_file", path, facts, false)
			},
			changed: "validated cleanup contract_file changed during the live run",
		},
	}
}

// TestBoundFileRoles_EachIdentityFactIsIndependent binds each frozen file role
// and changes one fact at a time: a replacement with the same bytes (identity),
// a same-length rewrite (digest), an append (size) and another accepted mode.
// Each is refused by the later check while the untouched file still verifies,
// and the bound bytes are exactly the file's. Aliases, unsafe modes, empty and
// oversized files and directories are refused at binding. Files owned by
// another user cannot be created without privilege and stay covered by the
// ownership branch alone.
func TestBoundFileRoles_EachIdentityFactIsIndependent(t *testing.T) { //nolint:gocognit,maintidx // One matrix of independent file facts and refusals per frozen role.
	t.Parallel()

	for _, role := range boundFileRoles() {
		t.Run(role.name, func(t *testing.T) {
			t.Helper()

			t.Parallel()

			arrange := func(t *testing.T) (string, []byte, string) {
				t.Helper()

				path := filepath.Join(t.TempDir(), "bound")
				body := role.body(t)

				if err := os.WriteFile(path, body, role.mode); err != nil {
					t.Fatal(err)
				}

				facts, bound, err := role.bind(path)
				if err != nil {
					t.Fatalf("bind: %v", err)
				}

				if !bytes.Equal(bound, body) {
					t.Fatal("bound bytes differ from the file")
				}

				if err := role.verify(path, facts); err != nil {
					t.Fatalf("unchanged file failed verification: %v", err)
				}

				return path, body, facts
			}

			for change, apply := range map[string]func(t *testing.T, path string, body []byte){
				"identity": func(t *testing.T, path string, body []byte) {
					t.Helper()

					if err := os.WriteFile(path+".new", body, role.mode); err != nil {
						t.Fatal(err)
					}

					if err := os.Rename(path+".new", path); err != nil {
						t.Fatal(err)
					}
				},
				"digest": func(t *testing.T, path string, body []byte) {
					t.Helper()

					changed := role.body(t)
					if role.name != "ca_file" {
						changed = bytes.ToUpper(body)
					}

					if err := os.Chmod(path, 0o600|role.mode&0o100); err != nil {
						t.Fatal(err)
					}

					if err := os.WriteFile(path, changed, 0); err != nil {
						t.Fatal(err)
					}

					if err := os.Chmod(path, role.mode); err != nil {
						t.Fatal(err)
					}
				},
				"size": func(t *testing.T, path string, body []byte) {
					t.Helper()

					if err := os.Chmod(path, 0o600|role.mode&0o100); err != nil {
						t.Fatal(err)
					}

					if err := os.WriteFile(path, append(bytes.Clone(body), '\n'), 0); err != nil {
						t.Fatal(err)
					}

					if err := os.Chmod(path, role.mode); err != nil {
						t.Fatal(err)
					}
				},
				"mode": func(t *testing.T, path string, _ []byte) {
					t.Helper()

					if err := os.Chmod(path, role.altMode); err != nil {
						t.Fatal(err)
					}
				},
			} {
				path, body, facts := arrange(t)
				apply(t, path, body)

				if err := role.verify(path, facts); err == nil || !strings.Contains(err.Error(), role.changed) {
					t.Errorf("%s change: %v, want %q", change, err, role.changed)
				}
			}

			for refusal, arrangeRefused := range map[string]func(t *testing.T, dir string) string{
				"alias": func(t *testing.T, dir string) string {
					t.Helper()

					target := filepath.Join(dir, "target")
					if err := os.WriteFile(target, role.body(t), role.mode); err != nil {
						t.Fatal(err)
					}

					link := filepath.Join(dir, "alias")
					if err := os.Symlink(target, link); err != nil {
						t.Fatal(err)
					}

					return link
				},
				"group writable": func(t *testing.T, dir string) string {
					t.Helper()

					path := filepath.Join(dir, "shared")
					if err := os.WriteFile(path, role.body(t), 0o600); err != nil {
						t.Fatal(err)
					}

					if err := os.Chmod(path, role.mode|0o020); err != nil {
						t.Fatal(err)
					}

					return path
				},
				"empty": func(t *testing.T, dir string) string {
					t.Helper()

					path := filepath.Join(dir, "empty")
					if err := os.WriteFile(path, nil, role.mode); err != nil {
						t.Fatal(err)
					}

					return path
				},
				"oversized": func(t *testing.T, dir string) string {
					t.Helper()

					path := filepath.Join(dir, "oversized")
					if err := os.WriteFile(path, role.body(t), 0o600); err != nil {
						t.Fatal(err)
					}

					// Both bounds are 1 MiB; a sparse extension needs no real write.
					if err := os.Truncate(path, max(maxCABytes, maxCleanupFileBytes)+1); err != nil {
						t.Fatal(err)
					}

					if err := os.Chmod(path, role.mode); err != nil {
						t.Fatal(err)
					}

					return path
				},
				"directory": func(t *testing.T, dir string) string {
					t.Helper()

					path := filepath.Join(dir, "directory")
					if err := os.Mkdir(path, 0o700); err != nil {
						t.Fatal(err)
					}

					return path
				},
			} {
				path := arrangeRefused(t, t.TempDir())
				if _, _, err := role.bind(path); !errors.Is(err, errs.ErrValidation) {
					t.Errorf("%s: bind = %v, want a validation refusal", refusal, err)
				}
			}
		})
	}
}

type localPreflight struct {
	contract, ca, command, recovery, launched, outputParent string
	caBody, commandBody                                     []byte
}

// arrangeLocalPreflight writes a synthetic contract whose CA, cleanup launcher
// and recovery file are owned temporary files, and arms the environment the
// preflight reads. The launcher only records that it ran; preflight must never
// run it.
func arrangeLocalPreflight(t *testing.T) localPreflight {
	t.Helper()

	dir := t.TempDir()
	p := localPreflight{
		contract:     filepath.Join(dir, "targets.json"),
		ca:           filepath.Join(dir, "ca.pem"),
		command:      filepath.Join(dir, "cleanup"),
		recovery:     filepath.Join(dir, "recovery.env"),
		launched:     filepath.Join(dir, "launched"),
		outputParent: t.TempDir(),
		caBody:       independentCAPEM(t),
	}
	p.commandBody = []byte(fmt.Sprintf("#!/bin/sh\n: >%q\n", p.launched))

	// t.TempDir honours the umask, which may leave the parent group-writable;
	// preflight rightly refuses that parent.
	if err := os.Chmod(p.outputParent, 0o700); err != nil { //nolint:gosec // Private test directory.
		t.Fatal(err)
	}

	if err := os.WriteFile(p.ca, p.caBody, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p.command, p.commandBody, 0o700); err != nil { //nolint:gosec // The launcher fixture must be owner-executable.
		t.Fatal(err)
	}

	if err := os.WriteFile(p.recovery, []byte("recovery\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	body := composeFixtureBody(t)
	body = replaceInFixture(t, body, "/opt/forge-lab/certs/forge-lab-ca.crt", p.ca)
	body = replaceInFixture(t, body, "/home/garga/.local/state/forge-lab/target-cleanup-run-neutral-fixture/cleanup", p.command)
	body = replaceInFixture(t, body, strings.Repeat("a", 64), hexDigest(p.commandBody))
	body = replaceInFixture(t, body, "/tmp/neutral-targets.run-neutral-fixture.recovery.env", p.recovery)
	body = strings.ReplaceAll(body, "2026-08-06T10:00:00Z", now.Add(-time.Minute).Format(time.RFC3339))
	body = replaceInFixture(t, body, `"expires_at": "2026-09-05"`, `"expires_at": "`+now.AddDate(0, 0, 10).Format(time.DateOnly)+`"`)

	if err := os.WriteFile(p.contract, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv(contractFileEnv, p.contract)
	t.Setenv(liveProfileEnv, "focused")
	t.Setenv(labRunnerForgesEnv, "gitlab,forgejo")
	t.Setenv(ownerEnvPrefix+"FORGEJO_OWNER", "fixture-user")
	t.Setenv(ownerEnvPrefix+"GITLAB_OWNER", "fixture-user")
	t.Setenv(ownerEnvPrefix+"FORGEJO_ENDPOINT", "")
	t.Setenv(ownerEnvPrefix+"GITLAB_ENDPOINT", "")
	// Stated independently of Identity(): the destructive confirmation an
	// operator types for exactly these two targets.
	t.Setenv(confirmDestroyEnv, "destroy-live-forge-fixtures|run=run-neutral-fixture|targets="+
		"forgejo@https://forgejo.compose.forgelab:8443/fixture-user#resources=rc-,"+
		"gitlab@https://gitlab.compose.forgelab:8443/fixture-user#resources=rc-")

	return p
}

// TestPrepareLiveInputs_LocalLifecycle runs the whole preflight against owned
// synthetic inputs. A success writes exactly the private snapshot, whose
// contract reloads with only the CA and launcher paths moved into it and whose
// facts bind the files actually written. A guard failure, a late CA failure
// after every guard passed, and an existing output directory leave no output
// of their own and preserve what was there. The cleanup launcher never runs.
// Not parallel: it arms the process environment.
func TestPrepareLiveInputs_LocalLifecycle(t *testing.T) { //nolint:gocognit // One subtest per lifecycle outcome over the same synthetic inputs.
	t.Run("success writes an exact private snapshot", func(t *testing.T) {
		t.Helper()

		p := arrangeLocalPreflight(t)
		output := filepath.Join(p.outputParent, "frozen")

		summary, err := PrepareLiveInputs(output, time.Now())
		if err != nil {
			t.Fatal(err)
		}

		if summary != (LivePreflightSummary{Selected: 2, Generation: "run-neutral-fixture"}) {
			t.Fatalf("summary = %+v", summary)
		}

		modes := map[string]os.FileMode{
			frozenContractName: 0o400, frozenContractFactsName: 0o400, frozenCAName: 0o400, frozenCAFactsName: 0o400,
			"cleanup": 0o500, cleanupCommandName: 0o400, cleanupCommandName + "-facts": 0o400, cleanupSourceCommandName: 0o400,
			cleanupContractFileName: 0o400, cleanupContractFileName + "-facts": 0o400,
		}

		entries, err := os.ReadDir(output)
		if err != nil {
			t.Fatal(err)
		}

		if len(entries) != len(modes) {
			t.Errorf("snapshot has %d entries, want %d: %v", len(entries), len(modes), entries)
		}

		for _, entry := range entries {
			info, infoErr := entry.Info()
			if want, known := modes[entry.Name()]; infoErr != nil || !known || !info.Mode().IsRegular() || info.Mode().Perm() != want {
				t.Errorf("snapshot entry %s: %v, %v, want a regular file with mode %v", entry.Name(), info, infoErr, modes[entry.Name()])
			}
		}

		if info, statErr := os.Stat(output); statErr != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("snapshot directory = %v, %v, want mode 0700", info, statErr)
		}

		frozenCA, frozenCommand := filepath.Join(output, frozenCAName), filepath.Join(output, "cleanup")
		requireFileBytes(t, frozenCA, p.caBody)
		requireFileBytes(t, frozenCommand, p.commandBody)

		source, err := loadLabContract(p.contract)
		if err != nil {
			t.Fatal(err)
		}

		source.CAFile.Value = &frozenCA
		source.Interfaces.CredentialCleanup.Command = frozenCommand

		frozen, err := loadLabContract(filepath.Join(output, frozenContractName))
		if err != nil || !reflect.DeepEqual(frozen, source) {
			t.Fatalf("frozen contract = %+v, %v\nwant the source with only the CA and launcher moved: %+v", frozen, err, source)
		}

		_, contractFacts, err := readPrivateContractBound(filepath.Join(output, frozenContractName))
		if err != nil {
			t.Fatal(err)
		}

		_, caFacts, err := readCAFileBound(frozenCA, nil)
		if err != nil {
			t.Fatal(err)
		}

		commandBinding, recoveryBinding, err := bindCleanupFiles(frozenCommand, nil, p.recovery)
		if err != nil {
			t.Fatal(err)
		}

		for name, want := range map[string]string{
			frozenContractFactsName:            contractFacts,
			frozenCAFactsName:                  caFacts,
			cleanupCommandName:                 frozenCommand,
			cleanupCommandName + "-facts":      commandBinding.Facts,
			cleanupSourceCommandName:           p.command,
			cleanupContractFileName:            p.recovery,
			cleanupContractFileName + "-facts": recoveryBinding.Facts,
		} {
			requireFileBytes(t, filepath.Join(output, name), []byte(want))
		}

		requireNotLaunched(t, p)
	})

	t.Run("a guard failure writes nothing", func(t *testing.T) {
		t.Helper()

		p := arrangeLocalPreflight(t)
		t.Setenv(confirmDestroyEnv, "destroy-live-forge-fixtures|run=other")

		output := filepath.Join(p.outputParent, "frozen")
		if _, err := PrepareLiveInputs(output, time.Now()); err == nil || !strings.Contains(err.Error(), confirmDestroyEnv+" must equal") {
			t.Fatalf("wrong confirmation = %v", err)
		}

		requireAbsent(t, output)
		requireNotLaunched(t, p)
	})

	t.Run("a late CA failure after every guard writes nothing", func(t *testing.T) {
		t.Helper()

		p := arrangeLocalPreflight(t)
		if err := os.WriteFile(p.ca, []byte("not a certificate\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		output := filepath.Join(p.outputParent, "frozen")
		if _, err := PrepareLiveInputs(output, time.Now()); err == nil || !strings.Contains(err.Error(), "PEM CERTIFICATE") {
			t.Fatalf("invalid CA = %v", err)
		}

		requireAbsent(t, output)
		requireNotLaunched(t, p)
	})

	t.Run("an existing output directory is preserved", func(t *testing.T) {
		t.Helper()

		p := arrangeLocalPreflight(t)
		output := filepath.Join(p.outputParent, "frozen")

		if err := os.Mkdir(output, 0o700); err != nil {
			t.Fatal(err)
		}

		canary := filepath.Join(output, "canary")
		if err := os.WriteFile(canary, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}

		if _, err := PrepareLiveInputs(output, time.Now()); err == nil || !strings.Contains(err.Error(), "create preflight output directory") {
			t.Fatalf("existing output = %v", err)
		}

		entries, err := os.ReadDir(output)
		if err != nil || len(entries) != 1 {
			t.Fatalf("existing output changed: %v, %v", entries, err)
		}

		requireFileBytes(t, canary, []byte("keep"))
		requireNotLaunched(t, p)
	})
}

func requireFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()

	got, err := os.ReadFile(path) //nolint:gosec // Owned test file.
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("%s = %q, %v, want %q", path, got, err, want)
	}
}

func requireAbsent(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists after a failed preflight: %v", path, err)
	}
}

func requireNotLaunched(t *testing.T, p localPreflight) {
	t.Helper()

	if _, err := os.Lstat(p.launched); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preflight executed the cleanup launcher: %v", err)
	}
}
