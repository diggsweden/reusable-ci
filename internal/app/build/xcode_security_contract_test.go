// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/stretchr/testify/require"
)

const (
	xcodeContractCertificate = "synthetic certificate role C31\x00\xff\r\nnot a real PKCS12"
	xcodeContractProfile     = "<plist>synthetic provisioning role P313\nnot a real profile</plist>\n"
	xcodeSigningSuccess      = "\u2713 Code signing configured successfully\n"
)

// Only this recording port is used. No security/Xcode binary, keychain or real
// credential is involved, and every ambient fallback belongs to t.TempDir.
func xcodeSecurityFixture(t *testing.T) appbuild.XcodeSetupCodeSigningInput {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	for _, key := range []string{"HOME", "TMPDIR", "TEMP", "TMP", "CI_TEMP_DIR", "RUNNER_TEMP"} {
		dir := filepath.Join(root, key)
		require.NoError(t, os.Mkdir(dir, 0o700))
		t.Setenv(key, dir)
	}

	tmp := filepath.Join(root, "explicit signing scratch")
	require.NoError(t, os.Mkdir(tmp, 0o700))

	return appbuild.XcodeSetupCodeSigningInput{
		CertBase64:       base64.StdEncoding.EncodeToString([]byte(xcodeContractCertificate)),
		PPBase64:         base64.StdEncoding.EncodeToString([]byte(xcodeContractProfile)),
		CertPassphrase:   "synthetic-cert-pass 'C7' $cert\ncert-line",
		KeychainPassword: "synthetic-keychain-pass \"K9\" ;key\nkey-line",
		TempDir:          tmp, ProvisioningProfilesDir: filepath.Join(root, "explicit installed profiles"),
	}
}

// These are independent wire expectations, not projections of the recorded
// calls. Passwords still enter argv: D246 is separate from diagnostic safety.
func xcodeSecurityVectors(in appbuild.XcodeSetupCodeSigningInput, tmp, keychainPassword string) [][]string {
	keychain := filepath.Join(tmp, "app-signing.keychain-db")

	return [][]string{
		{"create-keychain", "-p", keychainPassword, keychain},
		{"set-keychain-settings", "-lut", "21600", keychain},
		{"unlock-keychain", "-p", keychainPassword, keychain},
		{"import", filepath.Join(tmp, "certificate.p12"), "-P", in.CertPassphrase, "-A", "-t", "cert", "-f", "pkcs12", "-k", keychain},
		{"set-key-partition-list", "-S", "apple-tool:,apple:", "-k", keychainPassword, keychain},
		{"list-keychain", "-d", "user"},
		{"list-keychain", "-d", "user", "-s", keychain},
	}
}

// observedKeychainPassword returns the password this run generated, read from
// the create-keychain argv. Tests assert its properties rather than its value:
// the run mints it, so nothing outside the run knows it in advance.
func observedKeychainPassword(t *testing.T, calls [][]string) string {
	t.Helper()
	require.NotEmpty(t, calls, "no security call to read the generated password from")
	require.Equal(t, "create-keychain", calls[0][0])
	require.Len(t, calls[0], 4)

	return calls[0][2]
}

// requireGeneratedKeychainPassword pins what the generated password must be: not
// the operator's secret, not empty, and not something a process listing on the
// host could have predicted.
func requireGeneratedKeychainPassword(t *testing.T, supplied, generated string) {
	t.Helper()
	require.NotEmpty(t, generated)
	require.NotEqual(t, supplied, generated, "the operator's KEYCHAIN_PASSWORD must never reach security(1) argv")
	require.GreaterOrEqual(t, len(generated), 32, "the transient password must not be guessable")
}

// xcodeTeardownVectors is what compensating a setup that failed at stage
// failureAt must send. Nothing is undone before a keychain exists; the search
// list is restored only once it was actually replaced, which no failure in the
// setup matrix reaches.
func xcodeTeardownVectors(failureAt int, tmp string) [][]string {
	if failureAt == 0 {
		return nil
	}

	return [][]string{{"delete-keychain", filepath.Join(tmp, "app-signing.keychain-db")}}
}

func requireEmptyXcodeDir(t *testing.T, dir, msg string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err, msg)
	require.Empty(t, entries, msg)
}

type recordingXcodeSecurity struct {
	calls    [][]string
	contexts []context.Context
	run      func(int) (string, error)
}

func (sec *recordingXcodeSecurity) Run(ctx context.Context, args ...string) (string, error) {
	sec.calls = append(sec.calls, slices.Clone(args))

	sec.contexts = append(sec.contexts, ctx)
	if sec.run != nil {
		return sec.run(len(sec.calls) - 1)
	}

	return "", nil
}

type xcodeSecurityWriter struct {
	calls    []string
	accepted bytes.Buffer
	err      error
	short    bool
	observe  func()
}

func (out *xcodeSecurityWriter) Write(body []byte) (int, error) {
	out.calls = append(out.calls, string(body))
	if out.observe != nil {
		out.observe()
	}

	if out.err != nil {
		return 0, out.err
	}

	if out.short {
		return out.accepted.Write(body[:len(body)/2])
	}

	return out.accepted.Write(body)
}

func xcodeSecuritySecrets(in appbuild.XcodeSetupCodeSigningInput) []string {
	return []string{in.CertBase64, in.PPBase64, in.CertPassphrase, in.KeychainPassword, xcodeContractCertificate, xcodeContractProfile}
}

func xcodeSecurityCanaries(in appbuild.XcodeSetupCodeSigningInput) string {
	secrets := xcodeSecuritySecrets(in)

	values := make([]string, 0, 2*len(secrets))
	for _, secret := range secrets {
		values = append(values, secret, strconv.Quote(secret))
	}

	return strings.Join(values, "\n")
}

func assertXcodeSecurityNonleakage(t *testing.T, in appbuild.XcodeSetupCodeSigningInput, texts ...string) {
	t.Helper()

	for index, text := range texts {
		for _, secret := range xcodeSecuritySecrets(in) {
			if secret != "" {
				require.NotContains(t, text, secret, "secret in diagnostic channel %d", index)
				require.NotContains(t, text, strconv.Quote(secret), "quoted secret in diagnostic channel %d", index)
			}
		}
	}
}

// These tests mutate process globals and must not run in parallel. Regular
// owned files capture arbitrarily large writes without pipe-draining goroutines.
func captureXcodeProcessDiagnostics(t *testing.T, in appbuild.XcodeSetupCodeSigningInput) func() {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	files := make(map[string]*os.File)

	for _, name := range []string{"stdout", "stderr", "log", "slog"} {
		file, openErr := os.OpenFile(filepath.Join(root, name), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		require.NoError(t, openErr)
		t.Cleanup(func() { require.NoError(t, file.Close()) })

		files[name] = file
	}

	stdout, stderr := os.Stdout, os.Stderr
	logger, logWriter, flags, prefix := slog.Default(), log.Writer(), log.Flags(), log.Prefix()
	restored := false
	restore := func() {
		if restored {
			return
		}

		os.Stdout, os.Stderr = stdout, stderr
		// SetDefault can rewire the standard logger, so restore log last.
		slog.SetDefault(logger)
		log.SetOutput(logWriter)
		log.SetFlags(flags)
		log.SetPrefix(prefix)

		restored = true
	}
	t.Cleanup(restore) // Also restores on require.FailNow in a recording fake.

	os.Stdout, os.Stderr = files["stdout"], files["stderr"]
	slog.SetDefault(slog.New(slog.NewTextHandler(files["slog"], &slog.HandlerOptions{Level: slog.LevelDebug})))
	log.SetOutput(files["log"])

	return func() {
		restore()

		for name, file := range files {
			body, readErr := os.ReadFile(file.Name())
			require.NoError(t, readErr)
			assertXcodeSecurityNonleakage(t, in, string(body))
			require.Empty(t, string(body), "process %s bypassed injected diagnostic writers", name)
		}
	}
}

func TestXcodeSecurityContract_ProcessCaptureRestoresGlobals(t *testing.T) {
	stdout, stderr := os.Stdout, os.Stderr
	logger, logWriter, flags, prefix := slog.Default(), log.Writer(), log.Flags(), log.Prefix()

	for _, mode := range []string{"explicit_finish", "cleanup_fallback"} {
		t.Run(mode, func(t *testing.T) {
			finish := captureXcodeProcessDiagnostics(t, xcodeSecurityFixture(t))
			if mode == "explicit_finish" {
				finish()
				require.Same(t, stdout, os.Stdout)
				require.Same(t, stderr, os.Stderr)
				require.Same(t, logger, slog.Default())
				require.Equal(t, logWriter, log.Writer())
			}
		})
		require.Same(t, stdout, os.Stdout)
		require.Same(t, stderr, os.Stderr)
		require.Same(t, logger, slog.Default())
		require.Equal(t, logWriter, log.Writer())
		require.Equal(t, flags, log.Flags())
		require.Equal(t, prefix, log.Prefix())
	}
}

func TestXcodeSecurityContract_EveryFailureStopsAndDoesNotLeak(t *testing.T) { //nolint:gocognit // one six-stage matrix checks both app boundaries, cancellation, exact prefixes and all diagnostic channels.
	stages := []string{"create-keychain", "set-keychain-settings", "unlock-keychain", "security import", "set-key-partition-list", "read list-keychain", "list-keychain"}
	for failureAt, stage := range stages {
		for _, boundary := range []string{"setup", "release"} {
			for _, cancellation := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/cancel_%t", boundary, stage, cancellation), func(t *testing.T) {
					in := xcodeSecurityFixture(t)

					var release appbuild.XcodeReleaseBuildInput

					tmp, profiles := in.TempDir, in.ProvisioningProfilesDir

					if boundary == "release" {
						fsys, releaseInput := xcodeReleaseFixture(t)
						release = releaseInput
						release.CertBase64, release.PPBase64 = in.CertBase64, in.PPBase64
						release.CertPassphrase, release.KeychainPassword = in.CertPassphrase, in.KeychainPassword
						tmp = fsys.Path("TMPDIR")
						profiles = fsys.Path("HOME/Library/MobileDevice/Provisioning Profiles")
					}

					// The run mints the keychain password, so the expected argv is
					// only knowable once the first call has been recorded.
					var want, wantAll [][]string

					expect := func(calls [][]string) {
						if want != nil {
							return
						}

						want = xcodeSecurityVectors(in, tmp, observedKeychainPassword(t, calls))
						wantAll = append(slices.Clone(want[:failureAt+1]), xcodeTeardownVectors(failureAt, tmp)...)
					}

					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()

					cause := errors.New("unique security failure " + t.Name()) //nolint:err113 // per-stage identity is the contract.

					classified := errors.Join(cause, errs.ErrDependencyUnavailable)
					if cancellation {
						classified = errors.Join(classified, context.Canceled)
					}
					// A port error can quote argv or contain opaque tool text, just as
					// a wrapped OS error can. Do not rely on PEM-marker redaction.
					portErr := &os.PathError{Op: "synthetic security", Path: xcodeSecurityCanaries(in), Err: classified}
					sec, out, stderr, annotations := &recordingXcodeSecurity{}, &xcodeSecurityWriter{}, &xcodeSecurityWriter{}, &xcodeSecurityWriter{}
					ops, sink := &fakeXcodeBuild{}, fakeoutputsink.New(t)
					sec.run = func(index int) (string, error) {
						expect(sec.calls)
						require.Same(t, ctx, sec.contexts[index], "security context")
						require.Equal(t, wantAll[:index+1], sec.calls, "complete ordered security argv, then compensation")

						if index > failureAt {
							// Compensation, not continued setup. A canceled caller
							// must not stop the host from being restored.
							return "", nil
						}

						require.NoError(t, sec.contexts[index].Err())
						assertXcodeSecret(t, filepath.Join(tmp, "certificate.p12"), xcodeContractCertificate)
						assertXcodeSecret(t, filepath.Join(tmp, "pp.mobileprovision"), xcodeContractProfile)
						requireEmptyXcodeDir(t, profiles, "no early profile installation")
						require.NotContains(t, out.accepted.String(), "Code signing configured")

						if index == failureAt {
							if cancellation {
								cancel()
							}

							return "failed security output\n" + xcodeSecurityCanaries(in), portErr
						}

						return "successful security output\n" + xcodeSecurityCanaries(in), nil
					}
					beforeEnv := os.Environ()

					var err error

					prefix := ""

					checkProcess := captureXcodeProcessDiagnostics(t, in)
					if boundary == "setup" {
						err = appbuild.XcodeSetupCodeSigning(ctx, sec, out, in)

						checkProcess()
						require.Empty(t, out.calls, "failed setup must not attempt a success write")
					} else {
						err = appbuild.XcodeReleaseBuild(ctx, sink, sec, ops, output.NewAnnotator(annotations, output.FormatGitHub), out, stderr, release)

						checkProcess()

						prefix = "set up code signing: "

						require.Equal(t, []string{"IPA artifact: mobile-v2.0\n"}, out.calls)

						_, statErr := os.Lstat("build")
						require.ErrorIs(t, statErr, os.ErrNotExist)
					}

					require.ErrorIs(t, err, cause, "unique stage cause")
					require.ErrorIs(t, err, portErr, "original port error identity")
					require.ErrorIs(t, err, errs.ErrDependencyUnavailable)
					require.Equal(t, cancellation, errors.Is(err, context.Canceled))

					var pathErr *os.PathError
					require.ErrorAs(t, err, &pathErr)
					require.Same(t, portErr, pathErr)
					// Original causes remain inspectable by design. This does not
					// satisfy P313's stronger all-errors nonleakage guarantee.
					require.Contains(t, pathErr.Error(), in.KeychainPassword)
					requireGeneratedKeychainPassword(t, in.KeychainPassword, observedKeychainPassword(t, sec.calls))
					require.Equal(t, wantAll, sec.calls, "stop at the failed stage, then compensate")
					require.Empty(t, ops.calls)
					require.Empty(t, sink.Keys())
					require.Zero(t, sink.CloseCount())
					require.Empty(t, stderr.calls)
					require.Empty(t, annotations.calls)
					require.Equal(t, beforeEnv, os.Environ())
					requireEmptyXcodeDir(t, profiles, "no profile installation after failure")
					// Compensation removes every run-owned credential the failed
					// setup staged. The profiles directory itself is an ordinary
					// user directory and is deliberately left in place.
					for _, leaf := range []string{"certificate.p12", "pp.mobileprovision", "app-signing.keychain-db"} {
						_, leafErr := os.Lstat(filepath.Join(tmp, leaf))
						require.ErrorIs(t, leafErr, os.ErrNotExist, "compensation must remove "+leaf)
					}

					assertXcodeSecurityNonleakage(t, in, err.Error(), fmt.Sprintf("%v\n%+v", err, err), out.accepted.String(), stderr.accepted.String(), annotations.accepted.String(), fmt.Sprint(sink.AllScalar()))
					require.EqualError(t, err, prefix+stage+": security command failed")
				})
			}
		}
	}
}

func TestXcodeSecurityContract_ReleaseSuccessDoesNotPublishToolOutput(t *testing.T) {
	in := xcodeSecurityFixture(t)
	fsys, release := xcodeReleaseFixture(t)
	release.CertBase64, release.PPBase64 = in.CertBase64, in.PPBase64
	release.CertPassphrase, release.KeychainPassword = in.CertPassphrase, in.KeychainPassword
	release.XCConfigBase64 = ""

	const securityCalls = 8 // six to install, the search-list read, the teardown delete

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	sec := &recordingXcodeSecurity{}
	sec.run = func(index int) (string, error) {
		require.Less(t, index, securityCalls, "unexpected security command")
		require.Same(t, ctx, sec.contexts[index])

		return "opaque successful security output\n" + xcodeSecurityCanaries(in), nil
	}
	ops := &fakeXcodeBuild{run: func(args []string) {
		require.Equal(t, xcodeSecurityVectors(in, fsys.Path("TMPDIR"), observedKeychainPassword(t, sec.calls)), sec.calls,
			"security completes before Xcode")
		assertXcodeSecret(t, fsys.Path("TMPDIR/certificate.p12"), xcodeContractCertificate)
		assertXcodeSecret(t, fsys.Path("TMPDIR/pp.mobileprovision"), xcodeContractProfile)
		assertXcodeSecret(t, fsys.Path("HOME/Library/MobileDevice/Provisioning Profiles/pp.mobileprovision"), xcodeContractProfile)

		switch args[0] {
		case "archive":
			// A real xcarchive records the scheme it was built for, and the
			// release flow now compares that against the one it selected.
			// Writing a realistic plist here exercises the agreeing case; the
			// disagreeing one is covered in xcarchive_identity_test.go.
			fsys.WriteFile("checkout/build/app.xcarchive/Info.plist", []byte(xcarchiveInfoPlist("Zebra")))
		case "-exportArchive":
			fsys.WriteFile("checkout/build/export/Owned.ipa", []byte("synthetic IPA"))
		default:
			t.Fatalf("unexpected Xcode request: %v", args)
		}
	}}
	sink := fakeoutputsink.New(t)

	var stdout, stderr, annotations bytes.Buffer

	checkProcess := captureXcodeProcessDiagnostics(t, in)
	err := appbuild.XcodeReleaseBuild(ctx, sink, sec, ops, output.NewAnnotator(&annotations, output.FormatGitHub), &stdout, &stderr, release)

	checkProcess()
	require.NoError(t, err)
	// The owning build restores the host on the success path too: the read
	// search list was empty, so deleting the keychain is the whole restoration.
	want := xcodeSecurityVectors(in, fsys.Path("TMPDIR"), observedKeychainPassword(t, sec.calls))
	want = append(want, []string{"delete-keychain", fsys.Path("TMPDIR/app-signing.keychain-db")})
	require.Equal(t, want, sec.calls)
	requireGeneratedKeychainPassword(t, in.KeychainPassword, observedKeychainPassword(t, sec.calls))

	for _, leaf := range []string{"certificate.p12", "pp.mobileprovision", "app-signing.keychain-db"} {
		_, leafErr := os.Lstat(fsys.Path("TMPDIR/" + leaf))
		require.ErrorIs(t, leafErr, os.ErrNotExist, "successful build must not leave "+leaf)
	}

	requireEmptyXcodeDir(t, fsys.Path("HOME/Library/MobileDevice/Provisioning Profiles"), "installed profile must be removed")
	require.Len(t, ops.calls, 2)
	require.Equal(t, "IPA artifact: mobile-v2.0\n"+xcodeSigningSuccess+
		"Running: xcodebuild archive -workspace App.xcworkspace -scheme Zebra -configuration Release -archivePath build/app.xcarchive -destination generic/platform=iOS -skipPackagePluginValidation CURRENT_PROJECT_VERSION=999\n"+
		"Archive scheme verified: Zebra\nBuilt artifacts:\nbuild/app.xcarchive\nbuild/export/Owned.ipa\n", stdout.String())
	require.Equal(t, "Version: 2.0 (200)\n", stderr.String())
	require.Empty(t, annotations.String())
	require.Equal(t, map[string]string{"ipa-name": "mobile-v2.0", "version": "2.0", "build": "200"}, sink.AllScalar())
	assertXcodeSecurityNonleakage(t, in, stdout.String(), stderr.String(), annotations.String(), fmt.Sprint(sink.AllScalar()))
}

func TestXcodeSecurityContract_InvalidBlobsDoNotLeakOrStage(t *testing.T) {
	for _, field := range []string{"certificate", "profile"} {
		for _, kind := range []string{"missing", "malformed", "whitespace"} {
			t.Run(field+"/"+kind, func(t *testing.T) {
				in := xcodeSecurityFixture(t)
				bad := ""
				wantErr := errs.ErrPermissionDenied

				switch kind {
				case "malformed":
					bad = "%%%synthetic-invalid-" + field + "-secret"
					wantErr = errs.ErrMalformedInput
				case "whitespace":
					bad = " \t\r\n"
					wantErr = errs.ErrMalformedInput
				}

				if field == "certificate" {
					in.CertBase64 = bad
				} else {
					in.PPBase64 = bad
				}

				before := ownedTree(t, filepath.Dir(in.TempDir))
				sec, out := &recordingXcodeSecurity{}, &xcodeSecurityWriter{}
				checkProcess := captureXcodeProcessDiagnostics(t, in)
				err := appbuild.XcodeSetupCodeSigning(t.Context(), sec, out, in)

				checkProcess()
				require.ErrorIs(t, err, wantErr)
				require.Empty(t, sec.calls)
				require.Empty(t, out.calls)
				require.Equal(t, before, ownedTree(t, filepath.Dir(in.TempDir)))
				assertXcodeSecurityNonleakage(t, in, err.Error(), out.accepted.String())
			})
		}
	}
}

// TestXcodeSecurityContract_ProfileDestinationRefusalsAndCompensation covers the
// two profile-destination outcomes that matter: an unusable destination is
// refused before any keychain exists, and a destination that only becomes
// unusable after the keychain was installed is fully compensated.
func TestXcodeSecurityContract_ProfileDestinationRefusalsAndCompensation(t *testing.T) {
	for _, stage := range []string{"blocking_file", "occupied_directory", "occupied_link", "late_replacement"} {
		t.Run(stage, func(t *testing.T) {
			in := xcodeSecurityFixture(t)
			installed := filepath.Join(in.ProvisioningProfilesDir, "pp.mobileprovision")
			wantErr, wantContext := errs.ErrValidation, "must be a regular file"

			switch stage {
			case "blocking_file":
				require.NoError(t, os.WriteFile(in.ProvisioningProfilesDir, []byte("owned blocking file"), 0o600))

				wantContext = "open provisioning profiles dir"
			case "occupied_directory":
				require.NoError(t, os.MkdirAll(installed, 0o700))
			case "occupied_link":
				require.NoError(t, os.MkdirAll(in.ProvisioningProfilesDir, 0o700))
				require.NoError(t, os.Symlink(filepath.Join(in.TempDir, "pp.mobileprovision"), installed))
			case "late_replacement":
				wantContext = "install provisioning profile"
			}

			before := ownedTree(t, filepath.Dir(in.TempDir))
			sec, out := &recordingXcodeSecurity{}, &xcodeSecurityWriter{}

			sec.run = func(int) (string, error) { return xcodeSecurityCanaries(in), nil }
			if stage == "late_replacement" {
				// Break the destination only after the preflight accepted it, so
				// the failure lands with a keychain already installed.
				sec.run = func(index int) (string, error) {
					if index == 0 {
						require.NoError(t, os.Remove(in.ProvisioningProfilesDir))
						require.NoError(t, os.WriteFile(in.ProvisioningProfilesDir, []byte("owned late blocker"), 0o600))
					}

					return xcodeSecurityCanaries(in), nil
				}
			}

			checkProcess := captureXcodeProcessDiagnostics(t, in)
			err := appbuild.XcodeSetupCodeSigning(t.Context(), sec, out, in)

			checkProcess()
			require.ErrorIs(t, err, wantErr)
			require.ErrorContains(t, err, wantContext)
			require.Empty(t, out.calls)
			assertXcodeSecurityNonleakage(t, in, err.Error(), out.accepted.String())

			if stage == "late_replacement" {
				want := xcodeSecurityVectors(in, in.TempDir, observedKeychainPassword(t, sec.calls))
				want = append(want, []string{"delete-keychain", filepath.Join(in.TempDir, "app-signing.keychain-db")})
				require.Equal(t, want, sec.calls, "the installed keychain is deleted again")
				require.Equal(t, "owned late blocker", string(readXcodeFile(t, in.ProvisioningProfilesDir)))
			} else {
				require.Empty(t, sec.calls, "an unusable destination is refused before any security call")
				require.Equal(t, before, ownedTree(t, filepath.Dir(in.TempDir)), "a refused setup changes nothing")
			}

			for _, leaf := range []string{"certificate.p12", "pp.mobileprovision", "app-signing.keychain-db"} {
				_, leafErr := os.Lstat(filepath.Join(in.TempDir, leaf))
				require.ErrorIs(t, leafErr, os.ErrNotExist, "no run-owned credential may survive: "+leaf)
			}
		})
	}
}

func readXcodeFile(t *testing.T, path string) []byte {
	t.Helper()

	body, err := os.ReadFile(path)
	require.NoError(t, err)

	return body
}

func TestXcodeSecurityContract_InformationalWriterIsBestEffort(t *testing.T) {
	for _, kind := range []string{"error", "short_write"} {
		t.Run(kind, func(t *testing.T) {
			in := xcodeSecurityFixture(t)

			sec, out := &recordingXcodeSecurity{}, &xcodeSecurityWriter{}
			if kind == "error" {
				out.err = errors.New("synthetic writer failure\n" + xcodeSecurityCanaries(in)) //nolint:err113 // injected writer cause.
			} else {
				out.short = true
			}

			sec.run = func(int) (string, error) { return xcodeSecurityCanaries(in), nil }
			out.observe = func() {
				require.Equal(t, xcodeSecurityVectors(in, in.TempDir, observedKeychainPassword(t, sec.calls)), sec.calls)
				assertXcodeSecret(t, filepath.Join(in.ProvisioningProfilesDir, "pp.mobileprovision"), xcodeContractProfile)
			}
			// This informational write is deliberately ignored by the existing
			// app contract; it is not a transactional output sink or rollback.
			checkProcess := captureXcodeProcessDiagnostics(t, in)
			err := appbuild.XcodeSetupCodeSigning(t.Context(), sec, out, in)

			checkProcess()
			require.NoError(t, err)
			require.Equal(t, []string{xcodeSigningSuccess}, out.calls)

			wantAccepted := ""
			if kind == "short_write" {
				wantAccepted = xcodeSigningSuccess[:len(xcodeSigningSuccess)/2]
			}

			require.Equal(t, wantAccepted, out.accepted.String())
			assertXcodeSecurityNonleakage(t, in, strings.Join(out.calls, ""), out.accepted.String())
			assertXcodeSecret(t, filepath.Join(in.TempDir, "certificate.p12"), xcodeContractCertificate)
			assertXcodeSecret(t, filepath.Join(in.TempDir, "pp.mobileprovision"), xcodeContractProfile)
		})
	}
}
