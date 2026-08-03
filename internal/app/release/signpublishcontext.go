// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

// SignPublishContextInput drives `release sign-publish-context`.
type SignPublishContextInput struct {
	ReleaseSHA   string
	ReleaseTag   string
	ArtifactPath string
	EnvFile      string
	RunnerTemp   string
}

// SignPublishContext validates the release hand-off inputs, creates the
// per-job state directory, and appends the environment contract consumed by the
// sign/promote/publish steps.
func SignPublishContext(out io.Writer, in SignPublishContextInput) error {
	releaseSHA := strings.TrimSpace(in.ReleaseSHA)
	if !git.ValidCommitSHA(releaseSHA) {
		return fmt.Errorf("sign-publish-context: release-sha must be a 40- or 64-character lowercase commit hex digest: %w", errs.ErrUsage)
	}

	releaseTag := strings.TrimSpace(in.ReleaseTag)
	if !version.IsStableSemverTag(releaseTag) {
		return fmt.Errorf("sign-publish-context: release-tag must be stable vMAJOR.MINOR.PATCH: %w", errs.ErrUsage)
	}

	distDir := strings.TrimRight(strings.TrimSpace(in.ArtifactPath), "/")
	if distDir == "" {
		return fmt.Errorf("sign-publish-context: artifact-path is required: %w", errs.ErrUsage)
	}

	if strings.TrimSpace(in.EnvFile) == "" {
		return fmt.Errorf("sign-publish-context: env-file is required: %w", errs.ErrUsage)
	}

	stateDir, err := os.MkdirTemp(defaultReleaseRunnerTemp(in.RunnerTemp), "reusable-ci-sign-and-publish.*")
	if err != nil {
		return fmt.Errorf("sign-publish-context: create state dir: %w", err)
	}

	if err := os.Chmod(stateDir, 0o700); err != nil { //nolint:gosec // G302 false positive: 0o700 is the intentionally private mode for the per-job state directory.
		return fmt.Errorf("sign-publish-context: chmod state dir %s: %w", stateDir, err)
	}

	entries := []string{
		"RELEASE_TAG=" + releaseTag,
		"FORGEJO_REF_NAME=" + releaseTag,
		"RELEASE_SHA=" + releaseSHA,
		"DIST_DIR=" + distDir,
		// Neutral default basename (matches DefaultReleaseFilesManifest); a
		// consumer overrides the whole path via $RELEASE_FILES_MANIFEST.
		"RELEASE_FILES_MANIFEST=" + distDir + "/release-files.json",
		"SIGN_AND_PUBLISH_STATE_DIR=" + stateDir,
	}
	if err := appendReleaseEnvFile(in.EnvFile, entries...); err != nil {
		return err
	}

	if out != nil {
		_, _ = fmt.Fprintf(out, "sign-and-publish context exported for %s (%s)\n", releaseTag, releaseSHA)
	}

	return nil
}

// defaultReleaseRunnerTemp falls back to the OS temp dir when the run
// context carries no scratch directory.
//
// It does NOT consult $RUNNER_TEMP. value arrives from --runner-temp, whose
// sources are cienv.TempDir() ($CI_TEMP_DIR, $RUNNER_TEMP) — a superset of
// that one name — so an env read here could only ever re-answer what value
// already holds, and would reintroduce the CI_TEMP_DIR-blind precedence the
// flag exists to avoid. The app layer receives the run context; it does not
// go looking for it.
func defaultReleaseRunnerTemp(value string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}

	return os.TempDir()
}

func appendReleaseEnvFile(path string, entries ...string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec,mnd // CI runner env file directory chosen by caller.
		return fmt.Errorf("sign-publish-context: create env file dir %s: %w", filepath.Dir(path), err)
	}

	envFile, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // CI runner env file chosen by caller.
	if err != nil {
		return fmt.Errorf("sign-publish-context: open env file %s: %w", path, err)
	}

	defer func() { _ = envFile.Close() }()

	for _, entry := range entries {
		if _, err := fmt.Fprintln(envFile, entry); err != nil {
			return fmt.Errorf("sign-publish-context: append env entry to %s: %w", path, err)
		}
	}

	return nil
}
