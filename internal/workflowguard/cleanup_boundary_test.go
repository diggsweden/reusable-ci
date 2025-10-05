// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"context"
	"errors"
	"fmt"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func materializedCleanup(body []byte) (string, error) {
	var workflow eventWorkflow
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		return "", err
	}

	for name, job := range workflow.Jobs {
		materialize, lastBuild, cleanup := -1, -1, -1

		for index, step := range job.Steps {
			switch strings.TrimSpace(step.Run) {
			case "reusable-ci container materialize-build-secrets":
				materialize = index
			case "reusable-ci container build":
				lastBuild = index
			}

			if step.Name == "Remove materialized build secrets" {
				cleanup = index
			}
		}

		if cleanup < 0 {
			continue
		}

		// The whole job is scanned before deciding: a build added after the
		// cleanup step would mount secrets that are already gone -- or, on a
		// runner that skipped cleanup, read ones that should have been.
		step := job.Steps[cleanup]

		path := mappingChild(&step.Env, "BUILD_SECRETS_DIR")
		if materialize < 0 || lastBuild <= materialize || cleanup <= lastBuild || step.If != "${{ always() }}" || path == nil || path.Value != "${{ steps['build-secrets'].outputs['secret-dir'] }}" {
			return "", fmt.Errorf("%s: cleanup must follow every build, always run and use the materialized directory: %w", name, errs.ErrValidation)
		}

		return step.Run, nil
	}

	return "", fmt.Errorf("materialized secret cleanup step is missing: %w", errs.ErrValidation)
}

func checkMaterializedCleanup(t *testing.T, script string) {
	t.Helper()

	for _, previous := range []string{"true", "false"} {
		root := t.TempDir()
		secretDir := filepath.Join(root, "owned secrets")
		require.NoError(t, os.Mkdir(secretDir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(secretDir, "key"), []byte("synthetic"), 0o600))
		canary := filepath.Join(root, "keep")
		require.NoError(t, os.WriteFile(canary, []byte("outside cleanup"), 0o600))
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		cmd := exec.CommandContext(ctx, "/bin/bash", "--noprofile", "--norc", "-eu", "-c", previous+" || :\n"+script) //nolint:gosec // reviewed cleanup only, owned paths and closed environment.
		cmd.Dir = root
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + root, "TMPDIR=" + root, "BUILD_SECRETS_DIR=" + secretDir}
		output, err := cmd.CombinedOutput()

		cancel()
		require.NoError(t, err, string(output))
		_, err = os.Stat(secretDir)
		require.ErrorIs(t, err, os.ErrNotExist)
		body, err := os.ReadFile(canary)
		require.NoError(t, err)
		require.Equal(t, "outside cleanup", string(body))
	}
}

// TestMaterializedCleanup_RequiresCleanupAfterTheLastBuild feeds the guard
// small jobs. It used to return at the cleanup step, so a build placed after
// it -- consuming secrets the cleanup had already removed -- passed.
func TestMaterializedCleanup_RequiresCleanupAfterTheLastBuild(t *testing.T) {
	t.Parallel()

	const (
		materialize = "      - id: build-secrets\n        run: reusable-ci container materialize-build-secrets\n"
		build       = "      - run: reusable-ci container build\n"
		cleanup     = "      - name: Remove materialized build secrets\n        if: ${{ always() }}\n        env:\n          BUILD_SECRETS_DIR: ${{ steps['build-secrets'].outputs['secret-dir'] }}\n        run: rm -rf -- \"${BUILD_SECRETS_DIR}\"\n"
	)

	job := func(steps ...string) []byte {
		return []byte("jobs:\n  build:\n    steps:\n" + strings.Join(steps, ""))
	}

	for name, tc := range map[string]struct {
		body    []byte
		wantErr bool
	}{
		"cleanup after both builds":  {body: job(materialize, build, build, cleanup)},
		"a build after the cleanup":  {body: job(materialize, build, cleanup, build), wantErr: true},
		"cleanup before any build":   {body: job(materialize, cleanup, build), wantErr: true},
		"cleanup before materialize": {body: job(cleanup, materialize, build), wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := materializedCleanup(tc.body)
			if tc.wantErr != errors.Is(err, errs.ErrValidation) || (!tc.wantErr && err != nil) {
				t.Errorf("err = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}
