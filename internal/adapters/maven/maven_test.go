// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package maven_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/maven"
	"github.com/diggsweden/reusable-ci/internal/testutil/mockbinary"
)

func TestEvalExpression_ReturnsTrimmedStdout(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("mvn", `printf '1.2.3-SNAPSHOT\n'`)

	got, err := maven.New().EvalExpression(context.Background(), "project.version")
	if err != nil {
		t.Fatalf("EvalExpression: %v", err)
	}
	if want := "1.2.3-SNAPSHOT"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	invs := m.Invocations("mvn")
	if len(invs) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(invs))
	}
	want := []string{"help:evaluate", "-Dexpression=project.version", "-q", "-DforceStdout"}
	if !equal(invs[0].Args, want) {
		t.Errorf("args = %v, want %v", invs[0].Args, want)
	}
}

func TestEvalExpression_NonZeroExitIncludesOutput(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("mvn", `printf 'BOOM\n' >&2; exit 1`)

	_, err := maven.New().EvalExpression(context.Background(), "project.version")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "BOOM") {
		t.Errorf("error did not include stderr output: %v", err)
	}
}

func TestRunInherit_StreamsStdoutAndStderr(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("mvn", `printf 'building\n'; printf 'warn\n' >&2`)

	var stdout, stderr bytes.Buffer
	if err := maven.New().RunInherit(context.Background(), &stdout, &stderr, "clean", "package"); err != nil {
		t.Fatalf("RunInherit: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "building" {
		t.Errorf("stdout = %q, want %q", got, "building")
	}
	if got := strings.TrimSpace(stderr.String()); got != "warn" {
		t.Errorf("stderr = %q, want %q", got, "warn")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
