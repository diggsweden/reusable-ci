// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package changelog shells out to supported changelog renderers.
package changelog

import (
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
type Renderer struct {
	GitChglogBin string
	GitCliffBin  string
}

// New returns a default renderer.
func New() *Renderer { return &Renderer{} }

// RenderFull writes the full changelog to outputPath using backend.
func (r *Renderer) RenderFull(ctx context.Context, backend, config, tag, outputPath string) error {
	switch backend {
	case backendGitChglog:
		_, err := r.run(ctx, r.gitChglogBin(), "--config", config, "--next-tag", tag, "--output", outputPath)

		return err
	case backendGitCliff:
		_, err := r.run(ctx, r.gitCliffBin(), "--config", config, "--unreleased", "--tag", tag, "--output", outputPath)

		return err
	default:
		return fmt.Errorf("%w %q", errUnsupportedBackend, backend)
	}
}

// RenderBody returns the release bump commit body using backend.
func (r *Renderer) RenderBody(ctx context.Context, backend, config, tag string) (string, error) {
	switch backend {
	case backendGitChglog:
		return r.run(ctx, r.gitChglogBin(), "--config", config, "--next-tag", tag, tag)
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
	cmd.Env = changelogEnv(os.Environ())

	out, err := cmd.CombinedOutput()
	if err != nil {
		wrapped := safeexec.WrapError(err, bin, safeexec.FirstArg(args))
		if len(out) == 0 {
			return "", wrapped
		}

		return "", fmt.Errorf("%w\n%s", wrapped, safeexec.RedactKeyMaterial(out))
	}

	return strings.TrimRight(string(out), "\n"), nil
}

func changelogEnv(env []string) []string {
	drop := map[string]bool{
		"SSH_SIGNING_KEY":           true,
		"GPG_SIGNING_KEY":           true,
		"GPG_SIGNING_PASSWORD":      true,
		"GPG_PRIVATE_KEY":           true,
		"GPG_PASSPHRASE":            true,
		"COSIGN_SIGNING_KEY":        true,
		"COSIGN_SIGNING_PASSWORD":   true,
		"COSIGN_KEY":                true,
		"COSIGN_PASSWORD":           true,
		"FORGEJO_TOKEN":             true,
		"GITEA_TOKEN":               true,
		"GITHUB_TOKEN":              true,
		"GH_TOKEN":                  true,
		"REGISTRY_AUTH_FILE":        true,
		"REGISTRY_PASSWORD":         true,
		"FORGEJO_API_TOKEN":         true,
		"RELEASE_TOKEN":             true,
		"RELEASE_BOT_TOKEN":         true,
		"CODE_SCANNING_TOKEN":       true,
		"MAVEN_CENTRAL_PASSWORD":    true,
		"ANDROID_KEYSTORE_BASE64":   true,
		"SECRETS_PROPERTIES_BASE64": true,
	}

	out := env[:0]
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if ok && drop[name] {
			continue
		}

		out = append(out, entry)
	}

	return out
}
