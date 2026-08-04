// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// Some of what this product decides can only be observed from inside a job.
//
// Detection is the clearest case: Forgejo Actions sets GITHUB_* variables, so
// "does this resolve as forgejo or as github" has a different answer on a real
// runner than anywhere a host-run test can reach. The same is true of the
// annotation dialect, step summaries and output-file writes — all of them are
// decided by the runtime the runner provides, not by the forge API.
//
// So this tier pushes a workflow and reads what the runner made of it. The
// assertion lives INSIDE the job on purpose: a job that checks its own claim and
// exits non-zero turns the run's conclusion into the result, which avoids
// parsing logs whose format is a forge's private business and differs between
// them.

// workflowPath is where each forge looks for workflow definitions. Forgejo reads
// .forgejo/workflows first and falls back to .github/workflows; using its own
// directory keeps the fixture unambiguous about which runtime is meant.
func workflowPath(kind provider.Platform, name string) (string, error) {
	switch kind {
	case provider.PlatformForgejo:
		return ".forgejo/workflows/" + name + ".yml", nil
	case provider.PlatformGitHub:
		return ".github/workflows/" + name + ".yml", nil
	case provider.PlatformGitLab, provider.PlatformLocal:
	}

	return "", fmt.Errorf("no workflow layout for platform %q: %w", kind, errUnsupportedInRunner)
}

var errUnsupportedInRunner = errors.New("in-runner scenarios are not implemented for this platform")

// RunsInRunner reports whether the in-runner tier can drive this forge yet.
// GitLab's pipeline API is a different shape and is not wired up; saying so is
// better than a scenario that silently covers one forge while claiming parity.
func RunsInRunner(kind provider.Platform) bool {
	return kind == provider.PlatformForgejo
}

// RunWorkflow commits a workflow to the scratch repository, waits for the run it
// triggers, and returns the run's conclusion.
//
// The push itself is the trigger, so the workflow must be `on: [push]`. Waiting
// is bounded: a job that never starts and a job that failed have to end up
// different, and "still queued" forever is the outcome a missing runner produces.
func RunWorkflow(tb TB, target Target, repo, name, yaml string) string {
	tb.Helper()
	requireAccepted(tb, target)

	path, err := workflowPath(target.Kind, name)
	if err != nil {
		tb.Fatalf("livetest: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	endpoint := target.BaseURL() + "/api/v1/repos/" + target.Owner + "/" + repo + "/contents/" + path
	body := map[string]any{
		"content": base64.StdEncoding.EncodeToString([]byte(yaml)),
		"message": "livetest: " + name,
		"branch":  defaultBranch,
	}

	if _, err := decode(ctx, target, http.MethodPost, endpoint, body, nil, http.StatusCreated); err != nil {
		tb.Fatalf("livetest: commit workflow %s: %v", path, err)
	}

	return waitForRun(ctx, tb, target, repo, name)
}

// waitForRun polls until the run reaches a terminal state.
func waitForRun(ctx context.Context, tb TB, target Target, repo, name string) string {
	tb.Helper()

	endpoint := target.BaseURL() + "/api/v1/repos/" + target.Owner + "/" + repo + "/actions/tasks"

	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)

		var payload struct {
			Runs []struct {
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
			} `json:"workflow_runs"`
		}

		if _, err := decode(ctx, target, http.MethodGet, endpoint, nil, &payload, http.StatusOK); err != nil {
			continue
		}

		if len(payload.Runs) == 0 {
			continue
		}

		// Forgejo reports the terminal state in status; conclusion is populated
		// for some versions and empty for others, so status is the authority and
		// conclusion is only extra detail.
		switch payload.Runs[0].Status {
		case "success", "failure", "cancelled", "skipped":
			return payload.Runs[0].Status
		}
	}

	tb.Fatalf("livetest: workflow %q never reached a terminal state; a job that stays queued usually means no runner is registered for its labels",
		name)

	return ""
}
