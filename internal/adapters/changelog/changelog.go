// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package changelog shells out to supported changelog renderers.
package changelog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// Backend names accepted by RenderFull/RenderBody; they double as the
// conventional binary names when the test seams are unset.
const (
	backendGitChglog = "git-chglog"
	backendGitCliff  = "git-cliff"
)

// errUnsupportedBackend marks a backend name outside the supported set.
var errUnsupportedBackend = errors.New("unsupported changelog backend")

// Renderer invokes git-chglog or git-cliff. The binary fields are test seams;
// empty values use the conventional binary names.
//
// UnsetEnv removes credentials from the renderer's environment, the same way
// the syft and skopeo adapters take the signer boundary's list. A changelog
// renderer has no use for a token, and git-cliff reads GITLAB_TOKEN and
// GITHUB_TOKEN on its own if they are present; the caller supplies the one
// list the binary keeps of the credentials it resolves.
type Renderer struct {
	GitChglogBin string
	GitCliffBin  string
	UnsetEnv     []string
}

// New returns a default renderer.
func New() *Renderer { return &Renderer{} }

// RenderFull writes the full changelog to outputPath using backend.
func (r *Renderer) RenderFull(ctx context.Context, backend, config, tag, outputPath, repositoryURL string) error {
	switch backend {
	case backendGitChglog:
		_, err := r.run(ctx, r.gitChglogBin(), "--config", config, "--repository-url", repositoryURL, "--next-tag", tag, "--output", outputPath)

		return err
	case backendGitCliff:
		_, err := r.run(ctx, r.gitCliffBin(), "--config", config, "--unreleased", "--tag", tag, "--output", outputPath)

		return err
	default:
		return fmt.Errorf("%w %q", errUnsupportedBackend, backend)
	}
}

// RenderBody returns the release bump commit body using backend.
func (r *Renderer) RenderBody(ctx context.Context, backend, config, tag, repositoryURL string) (string, error) {
	switch backend {
	case backendGitChglog:
		return r.run(ctx, r.gitChglogBin(), "--config", config, "--repository-url", repositoryURL, "--next-tag", tag, tag)
	case backendGitCliff:
		return r.run(ctx, r.gitCliffBin(), "--config", config, "--unreleased", "--tag", tag)
	default:
		return "", fmt.Errorf("%w %q", errUnsupportedBackend, backend)
	}
}

func (r *Renderer) gitChglogBin() string {
	if r.GitChglogBin != "" {
		return r.GitChglogBin
	}

	return backendGitChglog
}

func (r *Renderer) gitCliffBin() string {
	if r.GitCliffBin != "" {
		return r.GitCliffBin
	}

	return backendGitCliff
}

func (r *Renderer) run(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := safeexec.Command(ctx, bin, args...)
	cmd.Env = envWithout(os.Environ(), r.UnsetEnv)

	// The rendered body is stdout alone. git-cliff writes warnings to stderr
	// while exiting 0, and they must not become part of a release commit
	// message; stderr travels only on the error.
	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		wrapped := safeexec.WrapError(err, bin, safeexec.FirstArg(args))
		if stderr.Len() == 0 {
			return "", wrapped
		}

		return "", fmt.Errorf("%w\n%s", wrapped, safeexec.RedactKeyMaterial(stderr.Bytes()))
	}

	return strings.TrimRight(string(out), "\n"), nil
}

func envWithout(env, names []string) []string {
	drop := make(map[string]bool, len(names))
	for _, name := range names {
		drop[name] = true
	}

	out := env[:0]
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if !drop[name] {
			out = append(out, entry)
		}
	}

	return out
}
