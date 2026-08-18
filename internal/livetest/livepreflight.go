// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

const (
	frozenContractName       = "targets.json"
	frozenContractFactsName  = "targets.json-facts"
	cleanupCommandName       = "cleanup-command"
	cleanupSourceCommandName = "cleanup-source-command"
	cleanupContractFileName  = "cleanup-contract-file"
	frozenCAName             = "ca.crt"
	frozenCAFactsName        = "ca.crt-facts"
	maxCABytes               = 1024 * 1024
	maxCleanupFileBytes      = 1024 * 1024
)

type cleanupFileBinding struct {
	Path  string
	Facts string
	Body  []byte
}

type caReadPhase uint8

const (
	caSourceInspected caReadPhase = iota
	caSourceRead
)

// LivePreflightSummary is credential-free output for the shell entrypoint.
type LivePreflightSummary struct {
	Selected   int
	Generation string
}

// PrepareLiveInputs validates the source contract and destructive authorization,
// then writes an owner-read-only run snapshot. It writes nothing until every
// contract, authority, token-lifecycle, cleanup, and confirmation check passes.
func PrepareLiveInputs(outputDir string, now time.Time) (LivePreflightSummary, error) { //nolint:cyclop // Ordered fail-closed preflight phases.
	lab, err := loadLabContract(os.Getenv(contractFileEnv))
	if err != nil {
		return LivePreflightSummary{}, err
	}

	cleanupCommand, cleanupContractFile := lab.cleanupPair()

	commandBinding, contractBinding, err := bindCleanupFiles(cleanupCommand, cleanupContractFile)
	if err != nil {
		return LivePreflightSummary{}, err
	}

	refs, err := selectedRefs(lab)
	if err != nil {
		return LivePreflightSummary{}, err
	}

	sourced := contract{
		runID:               lab.Generation.ID,
		refs:                refs,
		resourcePrefix:      ResourcePrefix,
		confirmation:        os.Getenv(confirmDestroyEnv),
		cleanupCommand:      cleanupCommand,
		cleanupContractFile: cleanupContractFile,
	}

	selected := 0

	for _, forge := range []provider.ForgeAPI{provider.ForgeGitLab, provider.ForgeForgejo} {
		owner := ownerFor(forge)
		if owner == "" {
			continue
		}

		endpoint, endpointErr := selectedEndpointFor(lab, forge)
		if endpointErr != nil {
			return LivePreflightSummary{}, endpointErr
		}

		if endpointErr = endpoint.requireLiveCapabilities(); endpointErr != nil {
			return LivePreflightSummary{}, endpointErr
		}

		target, targetErr := targetFromContract(lab, endpoint, forge, owner)
		if targetErr != nil {
			return LivePreflightSummary{}, targetErr
		}

		if validateErr := validate(target, sourced, endpoint.tokenMeta(lab.Generation.ID), now); validateErr != nil {
			return LivePreflightSummary{}, fmt.Errorf("%s destructive guard: %w", forge, validateErr)
		}

		selected++
	}

	if selected == 0 {
		return LivePreflightSummary{}, fmt.Errorf("no contract endpoint has an explicit RC_LIVE_<FORGE>_OWNER (gitlab or forgejo): %w", errs.ErrValidation)
	}

	caBody, err := readContractCA(lab)
	if err != nil {
		return LivePreflightSummary{}, err
	}

	if err := writeLiveSnapshot(outputDir, &lab, caBody, commandBinding, contractBinding); err != nil {
		return LivePreflightSummary{}, err
	}

	return LivePreflightSummary{Selected: selected, Generation: lab.Generation.ID}, nil
}

func targetFromContract(lab labContract, endpoint labEndpoint, forge provider.ForgeAPI, owner string) (Target, error) {
	host, err := endpoint.host()
	if err != nil {
		return Target{}, err
	}

	fulcioURL, oidcIssuer, _ := lab.fulcioFor(endpoint.Name)

	forgeAuthorities, err := endpoint.forgeAuthorities()
	if err != nil {
		return Target{}, err
	}

	registryAuthorities, err := endpoint.registryAuthorities()
	if err != nil {
		return Target{}, err
	}

	oidcAllowed, err := oidcAuthorities(fulcioURL)
	if err != nil {
		return Target{}, err
	}

	caFile := ""
	if lab.CAFile.Value != nil {
		caFile = *lab.CAFile.Value
	}

	target := Target{
		Forge:               forge,
		Host:                host,
		Owner:               owner,
		Token:               endpoint.Credential.Token,
		CredentialUsername:  endpoint.Credential.Username,
		RegistryOrigin:      endpoint.registryOrigin(),
		ForgeAuthorities:    forgeAuthorities,
		RegistryAuthorities: registryAuthorities,
		OIDCAuthorities:     oidcAllowed,
		Capabilities:        append([]string(nil), endpoint.Capabilities...),
		CAFile:              caFile,
		CAFacts:             os.Getenv(frozenCAFactsEnv),
		FulcioURL:           fulcioURL,
		OIDCIssuer:          oidcIssuer,
	}
	if err := validateSelectedEndpoint(endpoint, target); err != nil {
		return Target{}, err
	}

	return target, nil
}

func bindCleanupFiles(command, contractFile string) (cleanupFileBinding, cleanupFileBinding, error) {
	if err := validateAbsoluteDataPath("credential_cleanup.command", command); err != nil {
		return cleanupFileBinding{}, cleanupFileBinding{}, err
	}

	if err := validateAbsoluteDataPath("credential_cleanup.contract_file", contractFile); err != nil {
		return cleanupFileBinding{}, cleanupFileBinding{}, err
	}

	commandBinding, err := bindOwnedFile(command, true)
	if err != nil {
		return cleanupFileBinding{}, cleanupFileBinding{}, fmt.Errorf("credential cleanup command: %w", err)
	}

	contractBinding, err := bindOwnedFile(contractFile, false)
	if err != nil {
		return cleanupFileBinding{}, cleanupFileBinding{}, fmt.Errorf("credential cleanup contract_file: %w", err)
	}

	return commandBinding, contractBinding, nil
}

// VerifyCleanupBindings reopens the frozen command and original contract file
// and requires their identities and digests to match preflight.
func VerifyCleanupBindings(command, commandFacts, contractFile, contractFileFacts string) error {
	if err := verifyCleanupBinding("cleanup command", command, commandFacts, true); err != nil {
		return err
	}

	return verifyCleanupBinding("cleanup contract_file", contractFile, contractFileFacts, false)
}

// RunFrozenCleanup descriptor-pins the frozen launcher, immediately revalidates
// the producer-bound recovery path, and executes exactly that launcher/path pair.
func RunFrozenCleanup(command, commandFacts, contractFile, contractFileFacts string) error {
	commandFile, commandBinding, err := openOwnedFileBinding(command, true)
	if err != nil {
		return fmt.Errorf("validated frozen cleanup command changed during the live run: %w", err)
	}
	defer func() { _ = commandFile.Close() }()

	if commandBinding.Path != command || commandBinding.Facts != commandFacts {
		return fmt.Errorf("validated frozen cleanup command changed during the live run: %w", errs.ErrValidation)
	}

	if err := verifyCleanupBinding("cleanup contract_file", contractFile, contractFileFacts, false); err != nil {
		return err
	}

	// ExtraFiles keeps descriptor 3 open across exec. Executing through procfs
	// removes the pathname race for the launcher while preserving Forge Lab's
	// exact original contract_file argument.
	commandProcess := exec.CommandContext(context.Background(), "/proc/self/fd/3", contractFile) //nolint:gosec // Descriptor 3 is the preflight-bound frozen launcher.
	commandProcess.ExtraFiles = []*os.File{commandFile}
	commandProcess.Stdin = os.Stdin
	commandProcess.Stdout = os.Stdout
	commandProcess.Stderr = os.Stderr

	return commandProcess.Run()
}

func verifyCleanupBinding(label, path, expectedFacts string, executable bool) error {
	binding, err := bindOwnedFile(path, executable)
	if err != nil {
		return fmt.Errorf("validated %s changed during the live run: %w", label, err)
	}

	if binding.Path != path || binding.Facts != expectedFacts {
		return fmt.Errorf("validated %s changed during the live run: %w", label, errs.ErrValidation)
	}

	return nil
}

func bindOwnedFile(path string, executable bool) (cleanupFileBinding, error) {
	file, binding, err := openOwnedFileBinding(path, executable)
	if file != nil {
		_ = file.Close()
	}

	return binding, err
}

//nolint:cyclop,gosec // Every filesystem identity property is checked.
func openOwnedFileBinding(path string, executable bool) (*os.File, cleanupFileBinding, error) { //nolint:gocyclo // Ordered file identity checks are intentionally explicit.
	if err := validateNoSymlinkParent(path); err != nil {
		return nil, cleanupFileBinding{}, err
	}

	info, err := os.Lstat(path) //nolint:gosec // Clean absolute cleanup path is intentionally bound.
	if err != nil {
		return nil, cleanupFileBinding{}, err
	}

	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, cleanupFileBinding{}, fmt.Errorf("must be a regular non-symlink file: %w", errs.ErrValidation)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) { //nolint:gosec // Effective UIDs are non-negative on supported Unix runners.
		return nil, cleanupFileBinding{}, fmt.Errorf("must be owned by the current user: %w", errs.ErrValidation)
	}

	if info.Mode().Perm()&0o077 != 0 {
		return nil, cleanupFileBinding{}, fmt.Errorf("must not be accessible by group or other: %w", errs.ErrValidation)
	}

	if executable && info.Mode().Perm()&0o100 == 0 {
		return nil, cleanupFileBinding{}, fmt.Errorf("must be owner-executable: %w", errs.ErrValidation)
	}

	if !executable && info.Mode().Perm() != 0o400 && info.Mode().Perm() != 0o600 {
		return nil, cleanupFileBinding{}, fmt.Errorf("must have mode 0400 or 0600: %w", errs.ErrValidation)
	}

	if info.Size() < 1 || info.Size() > maxCleanupFileBytes {
		return nil, cleanupFileBinding{}, fmt.Errorf("must be non-empty and no larger than %d bytes: %w", maxCleanupFileBytes, errs.ErrValidation)
	}

	file, err := os.Open(path) //nolint:gosec // exact cleanup file is being bound for later revalidation.
	if err != nil {
		return nil, cleanupFileBinding{}, err
	}

	fail := func(err error) (*os.File, cleanupFileBinding, error) {
		_ = file.Close()

		return nil, cleanupFileBinding{}, err
	}

	opened, err := file.Stat()

	var openedStat *syscall.Stat_t

	openedStatOK := false
	if err == nil {
		openedStat, openedStatOK = opened.Sys().(*syscall.Stat_t)
	}

	if err != nil || !os.SameFile(info, opened) || info.Mode() != opened.Mode() ||
		!openedStatOK || openedStat.Uid != uint32(os.Geteuid()) { //nolint:gosec // Effective UIDs are non-negative on supported Unix runners.
		return fail(fmt.Errorf("changed while it was opened: %w", errs.ErrValidation))
	}

	body, err := io.ReadAll(io.LimitReader(file, maxCleanupFileBytes+1))
	if err != nil || len(body) != int(info.Size()) {
		return fail(fmt.Errorf("changed while it was read: %w", errs.ErrValidation))
	}

	after, err := file.Stat()

	var afterStat *syscall.Stat_t

	afterStatOK := false
	if err == nil {
		afterStat, afterStatOK = after.Sys().(*syscall.Stat_t)
	}

	if err != nil || !os.SameFile(opened, after) || opened.Size() != after.Size() ||
		opened.Mode() != after.Mode() || opened.ModTime() != after.ModTime() ||
		!afterStatOK || afterStat.Uid != uint32(os.Geteuid()) { //nolint:gosec // Effective UIDs are non-negative on supported Unix runners.
		return fail(fmt.Errorf("changed while it was read: %w", errs.ErrValidation))
	}

	pathAfter, err := os.Lstat(path) //nolint:gosec // Recheck the accepted pathname after reading its descriptor.
	if err != nil || !os.SameFile(info, pathAfter) {
		return fail(fmt.Errorf("path was replaced while it was read: %w", errs.ErrValidation))
	}

	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return fail(fmt.Errorf("path is not canonical: %w", errs.ErrValidation))
	}

	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return fail(fmt.Errorf("rewind bound file: %w", err))
	}

	facts, err := fileBindingFacts(info, body)
	if err != nil {
		return fail(err)
	}

	return file, cleanupFileBinding{Path: canonical, Facts: facts, Body: body}, nil
}

func fileBindingFacts(info os.FileInfo, body []byte) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("file identity is unavailable: %w", errs.ErrValidation)
	}

	digest := sha256.Sum256(body)

	return fmt.Sprintf("%d:%d:%x:%d:%d:%x", stat.Dev, stat.Ino, stat.Mode, stat.Uid, info.Size(), digest), nil
}

func readContractCA(lab labContract) ([]byte, error) { //nolint:cyclop // Bounded open/read/revalidation is intentionally explicit.
	if lab.CAFile.Value == nil {
		return nil, fmt.Errorf("selected local live targets require non-null ca_file: %w", errs.ErrValidation)
	}

	return readCAFile(*lab.CAFile.Value, nil)
}

func readCAFile(path string, phaseHook func(caReadPhase)) ([]byte, error) {
	body, _, err := readCAFileBound(path, phaseHook)

	return body, err
}

func readCAFileBound(path string, phaseHook func(caReadPhase)) ([]byte, string, error) { //nolint:cyclop,gocyclo // Ordered descriptor and pathname checks close source races.
	if err := validateNoSymlinkParent(path); err != nil {
		return nil, "", fmt.Errorf("ca_file: %w", err)
	}

	info, err := os.Lstat(path) //nolint:gosec // Validated contract CA path is intentionally frozen.
	if err != nil {
		return nil, "", fmt.Errorf("inspect ca_file: %w", err)
	}

	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 1 || info.Size() > maxCABytes {
		return nil, "", fmt.Errorf("ca_file must be a regular non-symlink file no larger than %d bytes: %w", maxCABytes, errs.ErrValidation)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid())) { //nolint:gosec // Effective UIDs are non-negative on supported Unix runners.
		return nil, "", fmt.Errorf("ca_file must be owned by root or the current user: %w", errs.ErrValidation)
	}

	if info.Mode().Perm()&0o022 != 0 {
		return nil, "", fmt.Errorf("ca_file must not be writable by group or other: %w", errs.ErrValidation)
	}

	if phaseHook != nil {
		phaseHook(caSourceInspected)
	}

	file, err := os.Open(path) //nolint:gosec // validated contract CA is frozen into private run state.
	if err != nil {
		return nil, "", fmt.Errorf("read ca_file: %w", err)
	}

	defer func() { _ = file.Close() }()

	opened, err := file.Stat()

	var openedStat *syscall.Stat_t

	openedStatOK := false
	if err == nil {
		openedStat, openedStatOK = opened.Sys().(*syscall.Stat_t)
	}

	if err != nil || !os.SameFile(info, opened) || info.Mode() != opened.Mode() ||
		!openedStatOK || openedStat.Uid != stat.Uid {
		return nil, "", fmt.Errorf("ca_file changed while it was opened: %w", errs.ErrValidation)
	}

	body, err := io.ReadAll(io.LimitReader(file, maxCABytes+1))
	if err != nil || len(body) != int(opened.Size()) || len(body) > maxCABytes {
		return nil, "", fmt.Errorf("read bounded non-empty ca_file: %w", errs.ErrValidation)
	}

	if phaseHook != nil {
		phaseHook(caSourceRead)
	}

	after, err := file.Stat()

	var afterStat *syscall.Stat_t

	afterStatOK := false
	if err == nil {
		afterStat, afterStatOK = after.Sys().(*syscall.Stat_t)
	}

	if err != nil || !os.SameFile(opened, after) || opened.Size() != after.Size() ||
		opened.Mode() != after.Mode() || opened.ModTime() != after.ModTime() ||
		!afterStatOK || afterStat.Uid != stat.Uid {
		return nil, "", fmt.Errorf("ca_file changed while it was read: %w", errs.ErrValidation)
	}

	pathAfter, err := os.Lstat(path) //nolint:gosec // Recheck the validated source pathname after reading its descriptor.
	if err != nil || !os.SameFile(info, pathAfter) {
		return nil, "", fmt.Errorf("ca_file path was replaced while it was read: %w", errs.ErrValidation)
	}

	if validationErr := validateCertificatePEM(body); validationErr != nil {
		return nil, "", validationErr
	}

	facts, err := fileBindingFacts(info, body)
	if err != nil {
		return nil, "", fmt.Errorf("bind ca_file facts: %w", err)
	}

	return body, facts, nil
}

//nolint:cyclop,gosec,govet // Atomic private snapshot writes keep each failure site explicit.
func writeLiveSnapshot(
	outputDir string,
	lab *labContract,
	caBody []byte,
	commandBinding cleanupFileBinding,
	contractBinding cleanupFileBinding,
) error {
	if !filepath.IsAbs(outputDir) || filepath.Clean(outputDir) != outputDir {
		return fmt.Errorf("preflight output directory must be a clean absolute path: %w", errs.ErrValidation)
	}

	if err := validateNoSymlinkParent(outputDir); err != nil {
		return fmt.Errorf("preflight output parent: %w", err)
	}

	parentInfo, err := os.Lstat(filepath.Dir(outputDir))
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("preflight output parent must be an existing non-symlink directory: %w", errs.ErrValidation)
	}

	if stat, ok := parentInfo.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Geteuid()) || parentInfo.Mode().Perm()&0o022 != 0 { //nolint:gosec // Effective UIDs are non-negative on supported Unix runners.
		return fmt.Errorf("preflight output parent must be safely owned by the current user: %w", errs.ErrValidation)
	}

	if err := os.Mkdir(outputDir, 0o700); err != nil {
		return fmt.Errorf("create preflight output directory: %w", err)
	}

	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(outputDir)
		}
	}()

	caPath := filepath.Join(outputDir, frozenCAName)
	if err := writePrivateFile(caPath, caBody); err != nil {
		return err
	}

	caBinding, err := bindOwnedFile(caPath, false)
	if err != nil {
		return fmt.Errorf("bind frozen ca_file: %w", err)
	}

	frozenCleanupPath := filepath.Join(outputDir, "cleanup")
	if err := writePrivateExecutable(frozenCleanupPath, commandBinding.Body); err != nil {
		return err
	}

	frozenCommandBinding, err := bindOwnedFile(frozenCleanupPath, true)
	if err != nil {
		return fmt.Errorf("bind frozen cleanup command: %w", err)
	}

	lab.CAFile.Value = &caPath
	if lab.Interfaces == nil || lab.Interfaces.CredentialCleanup == nil {
		return fmt.Errorf("credential cleanup interface disappeared before snapshot: %w", errs.ErrValidation)
	}

	lab.Interfaces.CredentialCleanup.Command = frozenCleanupPath

	body, err := json.MarshalIndent(lab, "", "  ")
	if err != nil {
		return fmt.Errorf("encode frozen live-target contract: %w", err)
	}

	body = append(body, '\n')

	contractPath := filepath.Join(outputDir, frozenContractName)
	if err := writePrivateFile(contractPath, body); err != nil {
		return err
	}

	frozenContractBinding, err := bindOwnedFile(contractPath, false)
	if err != nil {
		return fmt.Errorf("bind frozen live-target contract: %w", err)
	}

	if err := writePrivateFile(filepath.Join(outputDir, cleanupCommandName), []byte(frozenCommandBinding.Path)); err != nil {
		return err
	}

	if err := writePrivateFile(filepath.Join(outputDir, cleanupCommandName+"-facts"), []byte(frozenCommandBinding.Facts)); err != nil {
		return err
	}

	if err := writePrivateFile(filepath.Join(outputDir, cleanupSourceCommandName), []byte(commandBinding.Path)); err != nil {
		return err
	}

	if err := writePrivateFile(filepath.Join(outputDir, cleanupContractFileName), []byte(contractBinding.Path)); err != nil {
		return err
	}

	if err := writePrivateFile(filepath.Join(outputDir, cleanupContractFileName+"-facts"), []byte(contractBinding.Facts)); err != nil {
		return err
	}

	if err := writePrivateFile(filepath.Join(outputDir, frozenContractFactsName), []byte(frozenContractBinding.Facts)); err != nil {
		return err
	}

	if err := writePrivateFile(filepath.Join(outputDir, frozenCAFactsName), []byte(caBinding.Facts)); err != nil {
		return err
	}

	complete = true

	return nil
}

func validateNoSymlinkParent(path string) error {
	parent := filepath.Dir(path)

	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		return fmt.Errorf("parent path must not contain symbolic links: %w", errs.ErrValidation)
	}

	return nil
}

func writePrivateFile(path string, body []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400) //nolint:gosec // exact private mode is intentional.
	if err != nil {
		return fmt.Errorf("create private preflight file: %w", err)
	}

	if _, err = file.Write(body); err != nil {
		_ = file.Close()

		return fmt.Errorf("write private preflight file: %w", err)
	}

	if err = file.Close(); err != nil {
		return fmt.Errorf("close private preflight file: %w", err)
	}

	return nil
}

func writePrivateExecutable(path string, body []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o500) //nolint:gosec // exact private executable mode is intentional.
	if err != nil {
		return fmt.Errorf("create private preflight executable: %w", err)
	}

	if err = file.Chmod(0o500); err != nil {
		_ = file.Close()

		return fmt.Errorf("set private preflight executable mode: %w", err)
	}

	if _, err = file.Write(body); err != nil {
		_ = file.Close()

		return fmt.Errorf("write private preflight executable: %w", err)
	}

	if err = file.Close(); err != nil {
		return fmt.Errorf("close private preflight executable: %w", err)
	}

	return nil
}
