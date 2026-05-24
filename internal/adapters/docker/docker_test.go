// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package docker_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/docker"
	"github.com/diggsweden/reusable-ci/internal/testutil/mockbinary"
)

func TestNew(t *testing.T) {
	if docker.New() == nil {
		t.Fatal("New returned nil")
	}
}

func TestAdapter_RunCapturesOutput(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("docker", `printf 'out'; printf 'err' >&2`)
	a := &docker.Adapter{Bin: m.Path("docker")}

	stdout, stderr, err := a.Run(context.Background(), "buildx", "imagetools", "inspect", "img")
	if err != nil {
		t.Fatal(err)
	}

	if stdout != "out" || stderr != "err" {
		t.Errorf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestAdapter_RunInherit(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("docker", `printf 'docker %s\n' "$*"; printf 'warn\n' >&2`)
	a := &docker.Adapter{Bin: m.Path("docker")}

	var stdout, stderr bytes.Buffer
	if err := a.RunInherit(context.Background(), &stdout, &stderr, "buildx", "imagetools", "inspect", "img"); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stdout.String(), "docker buildx imagetools inspect img") {
		t.Errorf("stdout = %q", stdout.String())
	}

	if strings.TrimSpace(stderr.String()) != "warn" {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestAdapter_RunWrapsFailure(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("docker", `printf 'nope' >&2; exit 7`)
	a := &docker.Adapter{Bin: m.Path("docker")}

	_, stderr, err := a.Run(context.Background(), "buildx", "imagetools", "inspect", "img")
	if err == nil || !strings.Contains(err.Error(), "buildx") {
		t.Fatalf("err = %v", err)
	}

	if stderr != "nope" {
		t.Errorf("stderr = %q", stderr)
	}
}

// TestAdapter_RunRedactsPEMOnStderr pins the redactor on the stderr
// return path: a docker auth failure could in principle echo bearer
// or PEM-shaped credential material; the adapter must scrub it before
// returning to the caller.
func TestAdapter_RunRedactsPEMOnStderr(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("docker", `cat <<EOF >&2
auth failed:
-----BEGIN PRIVATE KEY-----
NEVER-SHOULD-LEAK
-----END PRIVATE KEY-----
EOF
exit 1`)
	a := &docker.Adapter{Bin: m.Path("docker")}

	_, stderr, err := a.Run(context.Background(), "login", "ghcr.io")
	if err == nil {
		t.Fatal("expected error")
	}

	if strings.Contains(stderr, "NEVER-SHOULD-LEAK") {
		t.Errorf("redactor missed PEM private-key block; stderr = %q", stderr)
	}

	if !strings.Contains(stderr, "redacted") {
		t.Errorf("redaction notice missing; stderr = %q", stderr)
	}
}
