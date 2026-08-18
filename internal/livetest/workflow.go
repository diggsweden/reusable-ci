// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
func workflowPath(forge provider.ForgeAPI, name string) (string, error) {
	switch forge {
	case provider.ForgeForgejo:
		return ".forgejo/workflows/" + name + ".yml", nil
	case provider.ForgeGitHub:
		return ".github/workflows/" + name + ".yml", nil
	case provider.ForgeGitLab:
		// GitLab reads one file at the repository root, so the scenario name
		// does not appear in the path; a second workflow would replace the
		// first rather than sit beside it.
		return ".gitlab-ci.yml", nil
	case provider.ForgeLocal:
	}

	return "", fmt.Errorf("no workflow layout for platform %q: %w", forge, errUnsupportedInRunner)
}

var errUnsupportedInRunner = errors.New("in-runner scenarios are not implemented for this platform")

// RunsInRunner reports whether the in-runner tier can drive this forge yet.
//
// Forgejo and GitLab both are: their pipeline APIs are different shapes and
// both are wired up, so every PAR-RUN-* scenario is genuinely two-forge rather
// than one forge with a claim of parity attached. GitHub is not, and that is an
// environment fact rather than a missing branch — this tier runs against a
// disposable lab, and there is no disposable GitHub.
//
// Scenarios must consult this and skip loudly instead of assuming, so a forge
// that cannot be driven is visible in the output rather than silently absent.
//
// This is a fact about the SUITE. Whether the environment in front of it has a
// runner is RunnerAvailable's question, and both have to hold.
func RunsInRunner(forge provider.ForgeAPI) bool {
	return forge == provider.ForgeForgejo || forge == provider.ForgeGitLab
}

// RunnerAvailable reports whether the operator says this environment has a
// runner that will pick up a job for the given forge.
//
// Separate from RunsInRunner because the two answer different questions, and
// only one of them is about the code. A road can deploy forges without
// deploying runners -- the lab's k3s road does exactly that until its
// ci-disposable overlay is applied -- and an in-runner scenario that assumes
// otherwise does not fail fast. It waits out a four-minute timeout and then
// reports "never reached a terminal state; a job that stays queued usually
// means no runner is registered for its labels", which sends the reader to look
// at labels on a road that deployed no runner at all. Ten scenarios doing that
// is forty minutes of wrong diagnosis.
//
// An absent operator input means no runner. Assuming one turns a road that
// cannot run jobs into a suite that reports failures about labels.
func RunnerAvailable(forge provider.ForgeAPI) bool {
	for _, deployed := range strings.Split(os.Getenv(labRunnerForgesEnv), ",") {
		if strings.EqualFold(strings.TrimSpace(deployed), string(forge)) {
			return true
		}
	}

	return false
}

// labRunnerForgesEnv is a separate operator-owned runner-availability input.
const labRunnerForgesEnv = "LAB_RUNNER_FORGES"

// RunWorkflow commits a workflow to the scratch repository, waits for the run it
// triggers, and returns the run's conclusion.
//
// The push itself is the trigger, so the workflow must be `on: [push]`. Waiting
// is bounded: a job that never starts and a job that failed have to end up
// different, and "still queued" forever is the outcome a missing runner produces.
func RunWorkflow(tb TB, target Target, repo, name, yaml string) string {
	tb.Helper()
	requireAccepted(tb, target)

	path, err := workflowPath(target.Forge, name)
	if err != nil {
		tb.Fatalf("livetest: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := commitFile(ctx, target, repo, path, "livetest: "+name, yaml); err != nil {
		tb.Fatalf("livetest: commit workflow %s: %v", path, err)
	}

	return waitForRun(ctx, tb, target, repo, name)
}

// commitFile writes one file to the default branch through whichever file API
// the forge offers.
//
// The two disagree on everything except the idea: Forgejo takes base64 under
// "content" with the path in the URL, GitLab takes plain text and calls the
// message "commit_message". Knowing that twice is how the shapes drift, so the
// scratch fixtures and the workflow fixtures share this one.
func commitFile(ctx context.Context, target Target, repo, path, message, content string) error {
	const (
		fieldBranch  = "branch"
		fieldContent = "content"
	)

	var endpoint string

	var body map[string]any

	switch target.Forge {
	case provider.ForgeGitLab:
		endpoint = target.BaseURL() + "/api/v4/projects/" +
			url.PathEscape(target.Owner+"/"+repo) + "/repository/files/" + url.PathEscape(path)
		body = map[string]any{
			fieldBranch:      defaultBranch,
			fieldContent:     content,
			"commit_message": message,
		}
	case provider.ForgeForgejo, provider.ForgeGitHub, provider.ForgeLocal:
		endpoint = target.BaseURL() + "/api/v1/repos/" + target.Owner + "/" + repo + "/contents/" + path
		body = map[string]any{
			fieldContent: base64.StdEncoding.EncodeToString([]byte(content)),
			"message":    message,
			fieldBranch:  defaultBranch,
		}
	}

	return discard(ctx, target, http.MethodPost, endpoint, body, http.StatusCreated)
}

// waitForRun polls until the run reaches a terminal state.
func waitForRun(ctx context.Context, tb TB, target Target, repo, name string) string {
	tb.Helper()

	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)

		status := latestRunStatus(ctx, target, repo)

		// Normalised, because the two forges spell the same outcomes
		// differently and a scenario should assert on the outcome rather than
		// on a vocabulary.
		switch status {
		case "success":
			return "success"
		case "failure", "failed":
			// Logged here rather than left to the caller: every caller would
			// have to remember, and the log is only fetchable before the
			// scratch repository is cleaned up.
			log := runLogTail(ctx, target, repo)
			if log != "" {
				tb.Logf("livetest: %s job log for %q (tail):\n%s", target.Forge, name, log)
			}

			// A job the runner never started cannot support the claim the caller
			// is about to make. Every caller reads "failure" as the product
			// having produced the wrong answer, so nineteen scenarios once
			// reported the detector, the annotations, and the step summary as
			// broken when the runner had simply failed to reach the Kubernetes
			// API. Same defect the log tail addresses: right verdict, wrong
			// vocabulary.
			if marker, ok := systemFailure(log); ok {
				tb.Fatalf("livetest: %s never ran the job for %q (%q); the runner failed to prepare it, "+
					"so this says nothing about the product -- the lab is what to look at",
					target.Forge, name, marker)
			}

			return "failure"
		case "cancelled", "canceled", "skipped":
			return status
		}
	}

	tb.Fatalf("livetest: workflow %q never reached a terminal state; a job that stays queued usually means no runner is registered for its labels",
		name)

	return ""
}

// latestRunStatus reads the most recent run's raw status, or "" while none is
// readable yet. A transport error is treated as "not yet": a pipeline is often
// not queryable in the moment between the push and its creation.
func latestRunStatus(ctx context.Context, target Target, repo string) string {
	switch target.Forge {
	case provider.ForgeGitLab:
		var pipelines []struct {
			Status string `json:"status"`
		}

		endpoint := target.BaseURL() + "/api/v4/projects/" +
			url.PathEscape(target.Owner+"/"+repo) + "/pipelines?per_page=1"
		if _, err := decodeQuiet(ctx, target, endpoint, &pipelines); err != nil || len(pipelines) == 0 {
			return ""
		}

		return pipelines[0].Status
	case provider.ForgeForgejo, provider.ForgeGitHub, provider.ForgeLocal:
		var payload struct {
			Runs []struct {
				Status string `json:"status"`
			} `json:"workflow_runs"`
		}

		endpoint := target.BaseURL() + "/api/v1/repos/" + target.Owner + "/" + repo + "/actions/tasks"
		if _, err := decodeQuiet(ctx, target, endpoint, &payload); err != nil || len(payload.Runs) == 0 {
			return ""
		}

		return payload.Runs[0].Status
	}

	return ""
}

// decodeQuiet is a GET whose failure is an ordinary "not yet" rather than a test
// failure, for the polling loop.
func decodeQuiet(ctx context.Context, target Target, endpoint string, into any) (int, error) {
	return decode(ctx, target, http.MethodGet, endpoint, nil, into, http.StatusOK)
}

// systemFailure reports whether a job log describes a job that never ran,
// returning the marker that says so.
//
// Only GitLab's wording is listed, because it is the only one observed. Forgejo
// surely has an equivalent, but inventing its phrasing would add a pattern
// nothing has ever matched — a check that cannot fire, which is the shape this
// file exists to avoid. Add it when a real log shows it.
func systemFailure(log string) (string, bool) {
	for _, marker := range []string{"Job failed (system failure)"} {
		if strings.Contains(log, marker) {
			return marker, true
		}
	}

	return "", false
}

// runLogTail returns the end of the failing run's job log, or "" when it cannot
// be read.
//
// An in-runner scenario that fails reports a conclusion and nothing else: the
// product ran inside a job, so the assertion sees "failure" and the reason stays
// on the forge. That is the same defect as a missing tool discovered mid-run —
// right answer, useless vocabulary — and it made every in-runner failure a
// re-run-by-hand exercise.
//
// Best effort by design. This runs on a path that is already failing, so a log
// that cannot be fetched must not replace the real verdict with an error about
// fetching logs.
func runLogTail(ctx context.Context, target Target, repo string) string {
	const keep = 4000

	endpoint, ok := runLogEndpoint(ctx, target, repo)
	if !ok {
		return ""
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}

	authorize(req, target)

	client, err := targetHTTPClient(target, 30*time.Second)
	if err != nil {
		return ""
	}

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	// Bounded: a job log can be megabytes, and the tail is where the failure is.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || len(body) == 0 {
		return ""
	}

	// Forgejo answers with a zip of one log per job; GitLab's job trace is plain
	// text. Decided by what the response says it is rather than by forge, so a
	// forge that changes its mind does not silently produce a screen of binary.
	if strings.Contains(resp.Header.Get("Content-Type"), "zip") {
		body = []byte(unzipFirst(body))
	}

	if len(body) > keep {
		body = body[len(body)-keep:]
	}

	return string(body)
}

// unzipFirst returns the contents of the first file in a zip, or "" when it
// cannot be read. Best effort, for the same reason as its caller.
func unzipFirst(archive []byte) string {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil || len(reader.File) == 0 {
		return ""
	}

	file, err := reader.File[0].Open()
	if err != nil {
		return ""
	}

	defer func() { _ = file.Close() }()

	// Bounded again: the compressed bound above says nothing about the
	// decompressed size.
	content, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil {
		return ""
	}

	return string(content)
}

// runLogEndpoint resolves where the latest run's log lives on this forge.
//
// Both forges need an id first, and neither spells it the same way: GitLab's log
// is a job trace under the project, Forgejo's is a run log under the repository
// (paths taken from the instance's own swagger rather than assumed).
func runLogEndpoint(ctx context.Context, target Target, repo string) (string, bool) {
	switch target.Forge {
	case provider.ForgeGitLab:
		var jobs []struct {
			ID int `json:"id"`
		}

		project := url.PathEscape(target.Owner + "/" + repo)
		if _, err := decodeQuiet(ctx, target, target.BaseURL()+"/api/v4/projects/"+project+"/jobs?per_page=1", &jobs); err != nil || len(jobs) == 0 {
			return "", false
		}

		return fmt.Sprintf("%s/api/v4/projects/%s/jobs/%d/trace", target.BaseURL(), project, jobs[0].ID), true
	case provider.ForgeForgejo, provider.ForgeGitHub, provider.ForgeLocal:
		var payload struct {
			Runs []struct {
				ID int `json:"id"`
			} `json:"workflow_runs"`
		}

		// /actions/runs, NOT the /actions/tasks the status poller uses: the two
		// endpoints return DIFFERENT id spaces for the same execution — measured,
		// task 352 against run 535 — so a task id here answers 404 "run with id
		// 352: resource does not exist", which reads as "no log yet".
		base := target.BaseURL() + "/api/v1/repos/" + target.Owner + "/" + repo
		if _, err := decodeQuiet(ctx, target, base+"/actions/runs", &payload); err != nil || len(payload.Runs) == 0 {
			return "", false
		}

		return fmt.Sprintf("%s/actions/runs/%d/logs", base, payload.Runs[0].ID), true
	}

	return "", false
}

// ReleaseAssetURL asks the forge where a release asset can be downloaded.
//
// The URL is read back rather than derived, because on GitLab it cannot be
// derived: the adapter uploads to project uploads, which mints a path containing
// a server-generated secret, and then links that URL to the release. GitLab does
// offer a predictable alternative in the generic packages API, but the URL a
// consumer actually follows is the one on the release, so asking for it tests
// the real path instead of a parallel one.
//
// Forgejo could be derived (releases/download/<tag>/<name>) and is still read
// back, so both forges answer the same question the same way and neither has a
// second source of truth to drift from.
func ReleaseAssetURL(tb TB, target Target, repo, tag, name string) string {
	tb.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	switch target.Forge {
	case provider.ForgeGitLab:
		var release struct {
			Assets struct {
				Links []struct {
					Name string `json:"name"`
					URL  string `json:"url"`
				} `json:"links"`
			} `json:"assets"`
		}

		endpoint := target.BaseURL() + "/api/v4/projects/" +
			url.PathEscape(target.Owner+"/"+repo) + "/releases/" + url.PathEscape(tag)
		if _, err := decodeQuiet(ctx, target, endpoint, &release); err != nil {
			tb.Fatalf("livetest: read release %s: %v", tag, err)
		}

		for _, link := range release.Assets.Links {
			if link.Name == name {
				return link.URL
			}
		}
	case provider.ForgeForgejo, provider.ForgeGitHub, provider.ForgeLocal:
		var release struct {
			Assets []struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			} `json:"assets"`
		}

		endpoint := target.BaseURL() + "/api/v1/repos/" + target.Owner + "/" + repo +
			"/releases/tags/" + url.PathEscape(tag)
		if _, err := decodeQuiet(ctx, target, endpoint, &release); err != nil {
			tb.Fatalf("livetest: read release %s: %v", tag, err)
		}

		for _, asset := range release.Assets {
			if asset.Name == name {
				return asset.URL
			}
		}
	}

	tb.Fatalf("livetest: release %s has no asset named %q", tag, name)

	return ""
}

// PublishBinaryAsset attaches the binary under test to a release, which is how
// it reaches a job. Named plainly ("reusable-ci") so the workflow fetching it
// does not have to know the build's filename.
func PublishBinaryAsset(tb TB, target Target, repo, tag, stageDir string) {
	tb.Helper()
	publishBinaryAssets(tb, target, repo, tag, stageDir, []binaryAsset{
		{source: Binary(tb), name: "reusable-ci"},
	})
}

// PublishKeylessAssets publishes the product and the separate static CONNECT
// proxy used to constrain the runner's OIDC token to its validated Fulcio.
func PublishKeylessAssets(tb TB, target Target, repo, tag, stageDir string) {
	tb.Helper()
	publishBinaryAssets(tb, target, repo, tag, stageDir, []binaryAsset{
		{source: Binary(tb), name: "reusable-ci"},
		{source: credentialProxyBinary(tb), name: "credential-proxy"},
	})
}

type binaryAsset struct {
	source string
	name   string
}

func publishBinaryAssets(tb TB, target Target, repo, tag, stageDir string, assets []binaryAsset) {
	tb.Helper()

	adapter := Provider(tb, target, repo)

	creator, ok := adapter.(provider.ReleaseCreator)
	if !ok {
		tb.Fatalf("livetest: %s cannot create releases", target.Forge)
	}

	staged := make([]string, 0, len(assets))
	for _, asset := range assets {
		path := filepath.Join(stageDir, asset.name)
		if err := copyFile(asset.source, path); err != nil {
			tb.Fatalf("livetest: stage %s: %v", asset.name, err)
		}

		staged = append(staged, path)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := creator.CreateRelease(ctx, RepoSlug(target, repo), provider.ReleaseSpec{
		Tag: tag, Name: "in-runner fixture", Assets: staged,
	}); err != nil {
		tb.Fatalf("livetest: publish binary asset: %v", err)
	}
}

func credentialProxyBinary(tb TB) string {
	tb.Helper()

	path := os.Getenv(proxyBinaryEnv)
	if !filepath.IsAbs(path) {
		tb.Fatalf("livetest: %s must be the absolute path of the built credential proxy", proxyBinaryEnv)
	}

	info, err := os.Stat(path) //nolint:gosec // Guarded lifecycle supplies this exact static helper path.
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		tb.Fatalf("livetest: %s is not an executable regular file", proxyBinaryEnv)
	}

	return path
}

func copyFile(from, to string) error {
	data, err := os.ReadFile(from) //nolint:gosec // the built product, handed over by the recipe.
	if err != nil {
		return err
	}

	return os.WriteFile(to, data, 0o755) //nolint:gosec // must be executable inside the job.
}

// TrustLabCA is the shell that installs the environment's CA into the job's
// trust store, so everything after it does ORDINARY TLS verification.
//
// The alternative was `curl -k` and npm's strict-ssl=false, on the reasoning
// that TLS trust is the environment's business and relaxing it belonged in the
// fixture rather than the product. True about where it belongs, wrong about
// what to do there: a fixture that steps around TLS can never catch a CA-trust
// regression, which is the failure this lab exists to surface — the same class
// lab_require_k3s_ca_current guards on the deployment side. The keyless probe
// has always had to do it properly, because cosign offers no such flag, so the
// honest pattern was already in the suite; only the other probes opted out.
//
// The CA comes from the parsed target contract rather than from the connection
// being tested — trusting whatever the endpoint serves would
// verify nothing. It is appended to the bundle rather than replacing it: both
// alpine- and debian-based job images read that path, and pointing
// SSL_CERT_FILE at the lab CA alone would break every public TLS client in the
// job, package managers included.
//
// The system bundle is not enough on its own, because a runtime that ships its
// own roots never reads it. Node is handled below; the JVM cannot be, since it
// needs a keytool import, so the Maven probe does that with LabCAPath. Any
// runtime added later needs the same question asked of it — "does this read
// /etc/ssl/certs?" — and the answer is often no.
//
// The validated PEM is base64-encoded before embedding. The alphabet cannot
// terminate shell quoting or introduce commands; no token or key is embedded.
func TrustLabCA(target Target) string {
	if !target.accepted {
		panic("livetest: refusing to load a CA through an unaccepted target")
	}

	if target.CAFile == "" {
		panic("livetest: accepted local target has no ca_file")
	}

	certificatePEM, err := loadTargetCAPEM(target)
	if err != nil {
		panic("livetest: contract ca_file is unreadable, so no probe can verify TLS: " + err.Error())
	}

	// Each client is told where the CA is, rather than the image's system bundle
	// being edited to contain it.
	//
	// Appending to /etc/ssl/certs/ca-certificates.crt needs root, and a job image
	// is under no obligation to give a probe root: quay.io/podman/stable started
	// running as a non-root user, and five GitLab scenarios then failed on
	// "Permission denied" writing that file — reported as the step summary, the
	// annotations and the output file all being broken at once, which is what a
	// prelude failure looks like from the outside.
	//
	// Pointing at the lab CA *alone* is deliberate, not a shortcut around
	// concatenating the system bundle. Everything these probes talk to is issued
	// by this CA, so a public root is never needed, and its absence turns
	// "accidentally reached the internet" into a TLS failure instead of a quiet
	// success. Nothing here depends on what the image happens to trust.
	//
	// Exported unconditionally: each costs nothing in an image without that
	// client, and a prelude that had to remember which image it was running in is
	// a prelude that will forget.
	encoded := base64.StdEncoding.EncodeToString(certificatePEM)

	return `printf '%s' '` + encoded + `' | base64 -d >` + LabCAPath + `
export SSL_CERT_FILE=` + LabCAPath + `
export CURL_CA_BUNDLE=` + LabCAPath + `
export GIT_SSL_CAINFO=` + LabCAPath + `
export REQUESTS_CA_BUNDLE=` + LabCAPath + `
# Node ships its own compiled-in root list and reads none of the above, so npm
# would still refuse the lab's registry.
export NODE_EXTRA_CA_CERTS=` + LabCAPath
}

// LabCAPath is where TrustLabCA leaves the CA inside the job, and the single
// path every client is pointed at.
//
// Some clients cannot be pointed at a file by environment variable at all: the
// JVM keeps its own truststore, so a Maven probe imports this path with keytool.
// Anything else needing the CA by path takes it from here rather than embedding
// a second copy.
const LabCAPath = "/tmp/lab-ca.crt"

// ProbeImage is the job image every in-runner probe runs in: podman/stable
// v5.6.2, by digest.
//
// One constant because five fixtures each spelled the image out, so changing it
// meant finding all five. By digest because a tag can be repointed, and an image
// that changes underneath a suite changes what the suite measures without any
// scenario changing — this one already runs as a non-root user, which is a
// property probes depend on and nothing would announce.
const ProbeImage = "quay.io/podman/stable@sha256:b4bdf91d79ef0396ec1c070faa395b8e879fd4da5b161a7522882619026d5fa4"

// ProbePrelude is the shell every in-runner probe starts with: it trusts the
// environment's CA, fetches the binary and defines run_product, which refuses to
// let a mistyped invocation look like a result.
//
// The kit already fails any host-run scenario the argument parser rejects, but
// that guard sees the process's stderr and an in-runner probe runs the binary
// inside a job, where the suite sees nothing but a conclusion. So the same rule
// has to be enforced on the far side of that boundary — and it is not
// theoretical: a step-summary probe was written against `report build`, which
// requires a subcommand, and its usage error was scored as summary output on one
// forge while the other correctly produced nothing. One red, one green, both
// meaningless.
//
// The `|| status=$?` is load-bearing rather than stylistic. Both runners execute
// job scripts under `set -e`, so a bare failing command aborts the shell at that
// line — which is *before* the output is printed. A probe whose product call
// failed therefore reported nothing at all: the job died with the product's exit
// code and an empty log, leaving no way to tell a real defect from a mistyped
// flag without re-running by hand. Making the call part of a compound command
// suspends `set -e` for it, so the diagnostics are always printed and the status
// is still returned to the caller.
func ProbePrelude(target Target, assetURL string) string {
	return TrustLabCA(target) + `
curl -fsSL -o reusable-ci "` + assetURL + `"
chmod +x reusable-ci

# Fails the job when the CLI could not parse the invocation, so a mistyped probe
# cannot be mistaken for the product's answer.
run_product() {
  status=0
  ./reusable-ci "$@" > product-out.txt 2>&1 || status=$?
  cat product-out.txt
  # Only a non-zero exit can be a parse failure: a command that completed
  # successfully parsed its arguments by definition, and its output may
  # legitimately quote these words -- a notice reporting the usage error it
  # deliberately degraded past reads exactly like a mistyped flag otherwise.
  if [ "$status" -ne 0 ] &&
     grep -qE 'usage error|flag provided but not defined|requires a subcommand|is required' product-out.txt; then
    echo "FAIL: the probe invoked the product incorrectly; this is a fixture bug, not a result"
    return 1
  fi
  if [ "$status" -ne 0 ]; then
    echo "FAIL: the product exited $status; see its output above"
  fi
  return $status
}`
}

// ReleaseAssetNames returns the names of every asset the forge lists on a
// release, sorted, or an empty slice when the release has none.
//
// The oracle for asset reconciliation: "what is attached now" is the only
// question that distinguishes an update from a duplicate, and it cannot be
// asked through the adapter under test without letting it grade its own work.
// The two forges keep assets in different places — GitLab links them to the
// release, the Gitea family attaches them — so the shape lives here rather than
// in a scenario.
func ReleaseAssetNames(tb TB, target Target, repo, tag string) []string {
	tb.Helper()
	requireAccepted(tb, target)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var names []string

	switch target.Forge {
	case provider.ForgeGitLab:
		var release struct {
			Assets struct {
				Links []struct {
					Name string `json:"name"`
				} `json:"links"`
			} `json:"assets"`
		}

		endpoint := target.BaseURL() + "/api/v4/projects/" +
			url.PathEscape(target.Owner+"/"+repo) + "/releases/" + url.PathEscape(tag)
		if _, err := decodeQuiet(ctx, target, endpoint, &release); err != nil {
			tb.Fatalf("livetest: read release %s: %v", tag, err)
		}

		for _, link := range release.Assets.Links {
			names = append(names, link.Name)
		}
	case provider.ForgeForgejo, provider.ForgeGitHub, provider.ForgeLocal:
		var release struct {
			Assets []struct {
				Name string `json:"name"`
			} `json:"assets"`
		}

		endpoint := target.BaseURL() + "/api/v1/repos/" + target.Owner + "/" + repo +
			"/releases/tags/" + url.PathEscape(tag)
		if _, err := decodeQuiet(ctx, target, endpoint, &release); err != nil {
			tb.Fatalf("livetest: read release %s: %v", tag, err)
		}

		for _, asset := range release.Assets {
			names = append(names, asset.Name)
		}
	}

	slices.Sort(names)

	return names
}
