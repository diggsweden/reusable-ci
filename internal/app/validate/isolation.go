// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

// IsolationInput drives Isolation.
type IsolationInput struct {
	// Workflow is the path to the workflow file to check (relative to FS when
	// FS is set, else an OS path).
	Workflow string
	// BuildJob is the artifact-producing job that must not see signing secrets.
	BuildJob string
	// SigningSecrets are the secret names forbidden in BuildJob.
	SigningSecrets []string
	// FS overrides filesystem access for tests. Nil -> the real OS filesystem.
	FS fs.FS
}

// Isolation asserts a workflow's SLSA Build L3 job-isolation invariants: the
// build job references no signing secret, and every actions/checkout step sets
// persist-credentials: false. Violations are emitted as error annotations
// (GitHub: `::error file=…,line=…::`; elsewhere a plain `Error:` line) and the
// call returns a wrapped errs.ErrValidation.
func Isolation(out io.Writer, annot output.Annotator, in IsolationInput) error { //nolint:varnamelen // idiomatic short names.
	data, err := readWorkflow(in)
	if err != nil {
		return err
	}

	violations, err := validate.CheckIsolation(data, validate.IsolationConfig{
		BuildJob:       in.BuildJob,
		SigningSecrets: in.SigningSecrets,
	})
	if err != nil {
		return fmt.Errorf("check isolation %s: %w", in.Workflow, err)
	}

	for _, v := range violations {
		annot.ErrorAt(output.Annotation{File: in.Workflow, Line: v.Line}, "%s", v.Msg)
	}

	if len(violations) > 0 {
		return fmt.Errorf("isolation validation failed for %s: %w", in.Workflow, errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(out, "%s SLSA Build L3 isolation invariants hold in %s\n", clicolor.Check(out), in.Workflow)

	return nil
}

func readWorkflow(in IsolationInput) ([]byte, error) {
	if in.FS != nil {
		data, err := fs.ReadFile(in.FS, in.Workflow)
		if err != nil {
			return nil, fmt.Errorf("read workflow %s: %w", in.Workflow, err)
		}

		return data, nil
	}

	data, err := os.ReadFile(in.Workflow)
	if err != nil {
		return nil, fmt.Errorf("read workflow %s: %w", in.Workflow, err)
	}

	return data, nil
}
