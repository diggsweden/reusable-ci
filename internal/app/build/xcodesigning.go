// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// XcodeSecurityOps abstracts the macOS `security` CLI for tests.
//
//nolint:iface // see SwiftFilesLister — distinct consumer-defined port.
type XcodeSecurityOps interface {
	Run(ctx context.Context, args ...string) (string, error)
}

// XcodeSetupCodeSigningInput drives XcodeSetupCodeSigning.
type XcodeSetupCodeSigningInput struct {
	CertBase64     string
	CertPassphrase string
	PPBase64       string // provisioning profile, base64
	// KeychainPassword is accepted for compatibility and no longer used. The
	// run generates its own; see newKeychainPassword.
	KeychainPassword string
	// TempDir is the location for transient files. Empty → $CI_TEMP_DIR
	// then $RUNNER_TEMP then os.TempDir().
	TempDir string
	// ProvisioningProfilesDir is where the provisioning profile gets
	// installed. Empty → ~/Library/MobileDevice/Provisioning Profiles.
	ProvisioningProfilesDir string
}

// XcodeSetupCodeSigning decodes a base64-encoded code-signing
// certificate + provisioning profile, creates a transient macOS
// keychain, imports the cert, and installs the profile.
//
// Pure darwin operation — the linux unit tests use a fake
// XcodeSecurityOps that records the security-CLI invocations.
func XcodeSetupCodeSigning(ctx context.Context, sec XcodeSecurityOps, w io.Writer, in XcodeSetupCodeSigningInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	cert, profile, err := decodeXcodeSigningSecrets(in)
	if err != nil {
		return err
	}
	// The standalone verb hands the installed keychain to a later job step, so
	// it deliberately keeps the state a failing setup would have compensated.
	_, err = setupXcodeSigning(ctx, sec, w, in, cert, profile)

	return err
}

// xcodeSigningState records exactly what one setup put on the host, so a failed
// setup and an owning caller can undo the same things. Each field is only set
// once its effect actually succeeded; the zero value tears nothing down.
type xcodeSigningState struct {
	certPath         string
	profilePath      string
	keychainPath     string
	installedProfile string
	priorKeychains   []string
	keychainCreated  bool
	searchListSet    bool
}

// xcodeSigningPaths is the complete set of destinations one setup will touch,
// resolved and checked before the first byte is written.
type xcodeSigningPaths struct {
	cert        string
	profile     string
	keychain    string
	profilesDir string
	installed   string
}

func setupXcodeSigning(ctx context.Context, sec XcodeSecurityOps, w io.Writer, in XcodeSetupCodeSigningInput, cert, profile []byte) (*xcodeSigningState, error) { //nolint:varnamelen // writer convention.
	paths, err := preflightXcodeSigningPaths(in)
	if err != nil {
		return nil, err
	}

	in.KeychainPassword, err = newKeychainPassword()
	if err != nil {
		return nil, err
	}

	state := &xcodeSigningState{certPath: paths.cert, profilePath: paths.profile, keychainPath: paths.keychain}
	if err := stageXcodeSigning(ctx, sec, in, paths, cert, profile, state); err != nil {
		// Compensate in reverse. The primary cause stays first so callers can
		// still tell why signing failed, not merely that cleanup ran.
		return nil, errors.Join(err, teardownXcodeSigning(ctx, sec, state))
	}

	_, _ = fmt.Fprintf(w, "%s Code signing configured successfully\n", clicolor.Check(w))

	return state, nil
}

func stageXcodeSigning(ctx context.Context, sec XcodeSecurityOps, in XcodeSetupCodeSigningInput, paths xcodeSigningPaths, cert, profile []byte, state *xcodeSigningState) error {
	if err := writeMobileSecret(paths.cert, cert); err != nil {
		return err
	}

	if err := writeMobileSecret(paths.profile, profile); err != nil {
		return err
	}

	if err := createXcodeKeychain(ctx, sec, paths.cert, paths.keychain, in, state); err != nil {
		return err
	}

	return installProvisioningProfile(paths.profile, paths.installed, state)
}

// preflightXcodeSigningPaths resolves every signing destination and refuses
// unsafe ones before any file, keychain or security(1) call exists. Roots are
// opened without following links, so a linked temporary or profiles component
// cannot redirect owner-only credential writes.
func preflightXcodeSigningPaths(in XcodeSetupCodeSigningInput) (xcodeSigningPaths, error) { //nolint:cyclop // one flat destination policy: staging root, three leaves, profiles root and their overlap.
	tmp := in.TempDir
	if tmp == "" {
		// CLI binding reads $CI_TEMP_DIR / $RUNNER_TEMP via flag sources;
		// fall back to the OS-level tmp dir for direct app-layer callers.
		var err error
		if tmp, err = mobileTempRoot(); err != nil {
			return xcodeSigningPaths{}, err
		}
	}

	paths := xcodeSigningPaths{
		cert:        filepath.Join(tmp, "certificate.p12"),
		profile:     filepath.Join(tmp, "pp.mobileprovision"),
		keychain:    filepath.Join(tmp, "app-signing.keychain-db"),
		profilesDir: in.ProvisioningProfilesDir,
	}

	staging, stagingErr := pathsafe.OpenRoot(tmp)
	if stagingErr != nil {
		return xcodeSigningPaths{}, fmt.Errorf("open signing staging directory %s: %w", tmp, stagingErr)
	}

	defer func() { _ = staging.Close() }()

	for _, name := range []string{"certificate.p12", "pp.mobileprovision"} {
		if leafErr := replaceableSigningLeaf(staging, name); leafErr != nil {
			return xcodeSigningPaths{}, leafErr
		}
	}
	// security(1) creates the keychain itself, so an existing name is a stale or
	// foreign keychain rather than something this run may adopt and later delete.
	if _, keychainErr := staging.Lstat("app-signing.keychain-db"); keychainErr == nil {
		return xcodeSigningPaths{}, fmt.Errorf("signing keychain %s must not already exist: %w", paths.keychain, errs.ErrValidation)
	} else if !errors.Is(keychainErr, os.ErrNotExist) {
		return xcodeSigningPaths{}, fmt.Errorf("inspect signing keychain %s: %w", paths.keychain, keychainErr)
	}

	if paths.profilesDir == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return xcodeSigningPaths{}, fmt.Errorf("user home dir: %w", homeErr)
		}

		paths.profilesDir = filepath.Join(home, "Library", "MobileDevice", "Provisioning Profiles")
	}

	leaf := filepath.Base(paths.profile)

	profiles, profilesErr := pathsafe.OpenRoot(paths.profilesDir)
	if profilesErr == nil {
		defer func() { _ = profiles.Close() }()

		if leafErr := replaceableSigningLeaf(profiles, leaf); leafErr != nil {
			return xcodeSigningPaths{}, leafErr
		}
		// Installing onto the staged file itself would give one destination two
		// owners: teardown could not tell the copy from its source.
		if sameSigningDir(staging.Name(), profiles.Name()) {
			return xcodeSigningPaths{}, fmt.Errorf("provisioning profiles dir must differ from the signing staging dir %s: %w", tmp, errs.ErrValidation)
		}

		paths.installed = filepath.Join(profiles.Name(), leaf)

		return paths, nil
	}

	if !errors.Is(profilesErr, os.ErrNotExist) {
		return xcodeSigningPaths{}, fmt.Errorf("open provisioning profiles dir %s: %w", paths.profilesDir, profilesErr)
	}
	// Creating the directory is the only mutation this preflight makes, and it
	// happens last so a refusal leaves the host exactly as it was. It runs here
	// rather than at install time so an unusable profiles path is refused before
	// any keychain exists.
	created, createErr := pathsafe.MkdirRoot(paths.profilesDir, 0o755) //nolint:gosec // macOS xcodebuild expects this exact dir mode.
	if createErr != nil {
		return xcodeSigningPaths{}, fmt.Errorf("create provisioning profiles dir %s: %w", paths.profilesDir, createErr)
	}
	defer func() { _ = created.Close() }()

	paths.installed = filepath.Join(created.Name(), leaf)

	return paths, nil
}

// A destination may be replaced only when it is an ordinary file this run can
// overwrite in place. Links, directories and devices are refused, not followed.
func replaceableSigningLeaf(root *os.Root, leaf string) error {
	info, err := root.Lstat(leaf)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("inspect signing destination %s: %w", filepath.Join(root.Name(), leaf), err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("signing destination %s must be a regular file: %w", filepath.Join(root.Name(), leaf), errs.ErrValidation)
	}

	return nil
}

func sameSigningDir(left, right string) bool {
	if left == right {
		return true
	}

	leftInfo, err := os.Stat(left)
	if err != nil {
		return false
	}

	rightInfo, err := os.Stat(right)
	if err != nil {
		return false
	}

	return os.SameFile(leftInfo, rightInfo)
}

// teardownXcodeSigning undoes every host change the recorded state actually
// made, in reverse order, and is safe after a partial setup. A profiles
// directory this run had to create is left in place: it is an ordinary user
// directory, not run-owned credential material. Removing the keychain also
// drops it from the search list, but the captured list is restored first so
// keychains this run displaced come back even if the delete fails.
func teardownXcodeSigning(ctx context.Context, sec XcodeSecurityOps, state *xcodeSigningState) error {
	if state == nil {
		return nil
	}

	var failures []error

	if state.installedProfile != "" {
		if err := os.Remove(state.installedProfile); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("remove installed provisioning profile: %w", err))
		}
	}

	failures = append(failures, restoreXcodeKeychainState(ctx, sec, state)...)
	for _, path := range []string{state.certPath, state.profilePath} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("remove staged signing material: %w", err))
		}
	}

	return errors.Join(failures...)
}

// restoreXcodeKeychainState puts the search list back and deletes the keychain
// this run created. An empty prior list is not something to restore: `-s` with
// no names would clear the search list outright, and deleting the keychain
// already removes this run's own entry, which is all that was ever added.
func restoreXcodeKeychainState(ctx context.Context, sec XcodeSecurityOps, state *xcodeSigningState) []error {
	var failures []error

	if state.searchListSet && len(state.priorKeychains) > 0 {
		if _, err := sec.Run(ctx, append([]string{securityListKeychain, "-d", "user", "-s"}, state.priorKeychains...)...); err != nil {
			failures = append(failures, xcodeSecurityError{stage: "restore " + securityListKeychain, cause: err})
		}
	}

	if state.keychainCreated {
		if _, err := sec.Run(ctx, "delete-keychain", state.keychainPath); err != nil {
			failures = append(failures, xcodeSecurityError{stage: "delete-keychain", cause: err})
		}
	}

	return failures
}

// validateXcodeSigningSecrets checks that the three secret inputs are
// non-empty. CLI binding pre-validates required flags; this is the
// belt-and-braces for direct app-layer callers.
func validateXcodeSigningSecrets(in XcodeSetupCodeSigningInput) error {
	if in.CertBase64 == "" {
		return fmt.Errorf("IOS_SIGNING_CERTIFICATE_BASE64 secret not found but enable-code-signing is true: %w", errs.ErrPermissionDenied)
	}

	if in.PPBase64 == "" {
		return fmt.Errorf("PROVISIONING_PROFILE_BASE64 secret not found but enable-code-signing is true: %w", errs.ErrPermissionDenied)
	}

	return nil
}

// newKeychainPassword mints the password for the keychain this run creates.
//
// security(1) takes keychain passwords only as argv, so whatever value is used
// is observable to a process listing on the build host. That argument is worth
// far less when the value protects nothing else: this keychain is created by
// this run, holds only the certificate it just imported, and is deleted when
// the build ends. Generating the password here means no operator secret is
// exposed, nothing has to be provisioned or rotated for it, and a value seen in
// a process listing is useless the moment the run finishes.
//
// A supplied KEYCHAIN_PASSWORD is accepted and ignored rather than refused, so
// existing callers keep working while the input is retired.
func newKeychainPassword() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate transient keychain password: %w", err)
	}

	return base64.RawStdEncoding.EncodeToString(raw), nil
}

func decodeXcodeSigningSecrets(in XcodeSetupCodeSigningInput) ([]byte, []byte, error) {
	if err := validateXcodeSigningSecrets(in); err != nil {
		return nil, nil, err
	}

	cert, err := decodeMobileSecret(in.CertBase64)
	if err != nil {
		return nil, nil, fmt.Errorf("decode certificate: %w: %w", err, errs.ErrMalformedInput)
	}

	profile, err := decodeMobileSecret(in.PPBase64)
	if err != nil {
		return nil, nil, fmt.Errorf("decode provisioning profile: %w: %w", err, errs.ErrMalformedInput)
	}

	return cert, profile, nil
}

// createXcodeKeychain runs the security(1) commands that create a transient
// keychain, unlock it, import the .p12, and make the resulting identity
// accessible to xcodebuild without further prompts. Each successful effect is
// recorded in state so a later failure, or the owning build, can undo it.
//
// Threat model for the password arguments: security(1) takes keychain and
// import passwords only as argv (-p/-P), with no file or stdin alternative, so
// they are observable to a process listing on the build host for the duration
// of each call. This code keeps them out of every channel it does control:
// diagnostics, logs, errors, summaries and job outputs. Removing the argv
// exposure itself needs a different tool, not a different call here.
func createXcodeKeychain(ctx context.Context, sec XcodeSecurityOps, certPath, keychainPath string, in XcodeSetupCodeSigningInput, state *xcodeSigningState) error {
	if _, err := sec.Run(ctx, "create-keychain", "-p", in.KeychainPassword, keychainPath); err != nil {
		return xcodeSecurityError{stage: "create-keychain", cause: err}
	}

	state.keychainCreated = true

	if _, err := sec.Run(ctx, "set-keychain-settings", "-lut", "21600", keychainPath); err != nil {
		return xcodeSecurityError{stage: "set-keychain-settings", cause: err}
	}

	if _, err := sec.Run(ctx, "unlock-keychain", "-p", in.KeychainPassword, keychainPath); err != nil {
		return xcodeSecurityError{stage: "unlock-keychain", cause: err}
	}

	if _, err := sec.Run(ctx,
		"import", certPath,
		"-P", in.CertPassphrase,
		"-A", "-t", "cert", "-f", "pkcs12", "-k", keychainPath); err != nil {
		return xcodeSecurityError{stage: "security import", cause: err}
	}

	if _, err := sec.Run(ctx,
		"set-key-partition-list",
		"-S", "apple-tool:,apple:",
		"-k", in.KeychainPassword, keychainPath); err != nil {
		return xcodeSecurityError{stage: "set-key-partition-list", cause: err}
	}

	// Read the search list before changing it. `-s` replaces the whole list, so
	// setting only this keychain would silently drop the host's own keychains
	// for the rest of the session and leave nothing to restore afterwards.
	prior, err := userKeychainSearchList(ctx, sec)
	if err != nil {
		return err
	}

	state.priorKeychains = prior

	search := prior
	if !slices.Contains(search, keychainPath) {
		search = append(slices.Clone(prior), keychainPath)
	}

	if _, err := sec.Run(ctx, append([]string{securityListKeychain, "-d", "user", "-s"}, search...)...); err != nil {
		return xcodeSecurityError{stage: securityListKeychain, cause: err}
	}

	state.searchListSet = true

	return nil
}

// A search list is a handful of keychains, not a stream to splice into argv.
const maxUserKeychains = 64

// One name for the search-list verb, shared by the read, the set and the restore.
const securityListKeychain = "list-keychain"

// userKeychainSearchList reads the current user search list. security(1) prints
// one quoted path per line. Its output is tool text rather than a trusted list
// of names, and every entry read here is handed straight back to it as
// arguments, so only quoted absolute control-free paths are accepted; anything
// else is not a keychain and is ignored.
func userKeychainSearchList(ctx context.Context, sec XcodeSecurityOps) ([]string, error) {
	out, err := sec.Run(ctx, securityListKeychain, "-d", "user")
	if err != nil {
		return nil, xcodeSecurityError{stage: "read " + securityListKeychain, cause: err}
	}

	var list []string

	for line := range strings.Lines(out) {
		name := strings.TrimSpace(line)
		if len(name) < 2 || !strings.HasPrefix(name, `"`) || !strings.HasSuffix(name, `"`) {
			continue
		}

		name = name[1 : len(name)-1]
		if !filepath.IsAbs(name) || strings.ContainsFunc(name, unicode.IsControl) {
			continue
		}

		list = append(list, name)
		if len(list) == maxUserKeychains {
			break
		}
	}

	return list, nil
}

// Port errors may contain passwords, argv or certificate/profile material.
// Keep the trusted stage printable and the cause available to errors.Is/As,
// without rendering opaque dependency diagnostics into app-owned errors.
type xcodeSecurityError struct {
	stage string
	cause error
}

func (err xcodeSecurityError) Error() string { return err.stage + ": security command failed" }
func (err xcodeSecurityError) Unwrap() error { return err.cause }

// installProvisioningProfile copies the staged .mobileprovision file to the
// destination the preflight already resolved and created, at 0o600.
func installProvisioningProfile(ppPath, installed string, state *xcodeSigningState) error {
	if err := copyMobileSecret(ppPath, installed); err != nil {
		return fmt.Errorf("install provisioning profile: %w", err)
	}

	state.installedProfile = installed

	return nil
}

func copyMobileSecret(src, dst string) error {
	body, err := os.ReadFile(src) //nolint:gosec // src is a CLI-flag path under operator control.
	if err != nil {
		return err
	}

	return writeMobileSecret(dst, body)
}
