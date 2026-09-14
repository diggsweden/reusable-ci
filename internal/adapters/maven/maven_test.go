// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package maven_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/maven"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

func TestEvalExpression_ReturnsTrimmedStdout(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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
	if !slices.Equal(invs[0].Args, want) {
		t.Errorf("args = %v, want %v", invs[0].Args, want)
	}
}

func TestEvalExpression_NonZeroExitIncludesOutput(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("mvn", `printf 'BOOM\n' >&2; exit 1`)

	_, err := maven.New().EvalExpression(context.Background(), "project.version")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("non-zero exit = %v, want ErrValidation", err)
	}

	if !strings.Contains(err.Error(), "BOOM") {
		t.Errorf("error did not include stderr output: %v", err)
	}
}

// TestEvalExpression_RedactsPEMInError pins the redactor on the error
// path: if a future maven plugin echoes private-key material on stderr
// during a failed help:evaluate (extremely unlikely but defended), the
// wrapped error must not propagate the key bytes.
func TestEvalExpression_RedactsPEMInError(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("mvn", `cat <<EOF >&2
-----BEGIN PGP PRIVATE KEY BLOCK-----
NEVER-SHOULD-LEAK
-----END PGP PRIVATE KEY BLOCK-----
EOF
exit 1`)

	_, err := maven.New().EvalExpression(context.Background(), "project.version")
	if err == nil {
		t.Fatal("expected error")
	}

	if strings.Contains(err.Error(), "NEVER-SHOULD-LEAK") {
		t.Errorf("redactor missed PEM private-key block; err = %v", err)
	}

	if !strings.Contains(err.Error(), "redacted") {
		t.Errorf("redaction notice missing; err = %v", err)
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

// TestEvalExpression_ValueIsStdoutAlone covers a JVM notice on stderr. With a
// combined read, "Picked up JAVA_TOOL_OPTIONS: ..." came back as the first
// line of the project version and reached the SBOM subject and the release
// summary; the value is stdout, and stderr travels only on the error.
func TestEvalExpression_ValueIsStdoutAlone(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("mvn", `printf 'Picked up JAVA_TOOL_OPTIONS: -Dfile.encoding=UTF-8\n' >&2; printf '1.2.3'`)

	a := &maven.Adapter{MvnBin: m.Path("mvn")}

	got, err := a.EvalExpression(context.Background(), "project.version")
	if err != nil || got != "1.2.3" {
		t.Fatalf("EvalExpression = %q, %v; want the stdout value alone", got, err)
	}
}

// TestRunInheritIn_RunsInsideTheGivenDirectory: a multi-module release runs
// mvn in the module's directory, so the directory decides which pom is
// built; an empty directory means the current one.
func TestRunInheritIn_RunsInsideTheGivenDirectory(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("mvn", `pwd`)

	dir := t.TempDir()

	var stdout bytes.Buffer
	if err := (&maven.Adapter{MvnBin: m.Path("mvn")}).RunInheritIn(context.Background(), dir, &stdout, &bytes.Buffer{}, "package"); err != nil {
		t.Fatal(err)
	}

	if got := strings.TrimSpace(stdout.String()); got != dir {
		t.Errorf("mvn ran in %q, want %q", got, dir)
	}
}
