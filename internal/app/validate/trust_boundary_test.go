// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestTokenTrustBoundary_RedactionAndClassifications(t *testing.T) {
	t.Parallel()

	const token = "INVENTED_PROVIDER_CREDENTIAL"

	for _, cause := range []error{nil, errs.ErrPermissionDenied, errs.ErrDependencyUnavailable, context.Canceled, context.DeadlineExceeded} {
		prov := fakeprovider.New(t).WithPlatform(provider.ForgeGitLab)
		if cause != nil {
			prov = prov.WithValidateTokenError(fmt.Errorf("safe provider context %s: %w", token, cause))
		}

		var out bytes.Buffer

		err := appvalidate.Token(t.Context(), prov, &out, appvalidate.TokenInput{Token: token, Repository: "owner/repository"})

		calls := prov.ValidateTokenCalls()
		if len(calls) != 1 || calls[0].Token != token || calls[0].Repo != "owner/repository" {
			t.Fatalf("token request not forwarded: %+v", calls)
		}

		if cause == nil {
			if err != nil || !strings.Contains(out.String(), "token validated") {
				t.Fatalf("positive err=%v output=%s", err, &out)
			}
		} else if !errors.Is(err, cause) || !strings.Contains(err.Error(), "safe provider context") || strings.Contains(out.String(), "token validated") {
			t.Fatalf("cause=%v err=%v out=%s", cause, err, &out)
		}

		text := out.String()
		if err != nil {
			text += err.Error()
		}

		if strings.Contains(text, token) {
			t.Fatalf("credential leaked: %s", text)
		}

		if cause != nil && errs.ExitCodeFromError(err) != errs.ExitCodeFromError(cause) {
			t.Fatalf("classification changed: %v", err)
		}
	}
}

func TestWorkspaceTrustBoundary_DirectoryAndManifestLinks(t *testing.T) { //nolint:gocognit // table owns both ecosystems' positive files, link variants, classifications and unchanged canaries.
	for _, kind := range []string{"regular", "directory", "parent", "manifest"} {
		fsys := testfs.NewReal(t)
		outside := testfs.NewReal(t)

		fsys.Chdir()
		outside.WriteFile("Cargo.lock", []byte("lock canary"))
		outside.WriteFile("rust-toolchain.toml", []byte("pin canary"))

		const pom = `<project><properties><project.build.outputTimestamp>1</project.build.outputTimestamp></properties></project>`
		outside.WriteFile("pom.xml", []byte(pom))

		dir := "project"

		switch kind {
		case "regular":
			fsys.WriteFile("project/Cargo.lock", []byte("lock"))
			fsys.WriteFile("project/rust-toolchain.toml", []byte(`[toolchain]
channel = "1.90.0"`))
			fsys.WriteFile("project/pom.xml", []byte(pom))
		case "directory":
			if err := os.Symlink(outside.Root, fsys.Path(dir)); err != nil {
				t.Fatal(err)
			}
		case "parent":
			fsys.MkdirAll("parent")

			if err := os.Symlink(outside.Root, fsys.Path("parent/link")); err != nil {
				t.Fatal(err)
			}

			dir = "parent/link"
		case "manifest":
			fsys.MkdirAll(dir)

			for _, name := range []string{"Cargo.lock", "rust-toolchain.toml", "pom.xml"} {
				if err := os.Symlink(outside.Path(name), fsys.Path(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
		}

		plan := validationPlanJSON(t, validationConfigPlan(t,
			config.Artifact{Name: "jvm", ProjectType: projecttype.Maven, WorkingDirectory: dir},
			config.Artifact{Name: "crate", ProjectType: projecttype.Cargo, WorkingDirectory: dir}))

		var out bytes.Buffer

		cargoErr := appvalidate.CargoPrerequisites(t.Context(), fakeCargoTool{version: "cargo fixture"}, &out, output.Annotator{}, appvalidate.CargoPrerequisitesInput{ConfigPlanJSON: plan})

		jvmErr := appvalidate.JVMReproducibility(t.Context(), &out, output.Annotator{}, appvalidate.JVMReproducibilityInput{ConfigPlanJSON: plan})
		if kind == "regular" {
			if cargoErr != nil || jvmErr != nil {
				t.Fatalf("positive control: cargo=%v jvm=%v", cargoErr, jvmErr)
			}
		} else {
			jvmClass := errs.ErrInvalidConfig
			if kind == "manifest" {
				jvmClass = errs.ErrValidation
			}

			if !errors.Is(cargoErr, errs.ErrInvalidConfig) || !errors.Is(jvmErr, jvmClass) {
				t.Fatalf("link %s approved/unclassified: cargo=%v jvm=%v", kind, cargoErr, jvmErr)
			}
		}

		if string(outside.ReadFile("Cargo.lock")) != "lock canary" || string(outside.ReadFile("rust-toolchain.toml")) != "pin canary" || string(outside.ReadFile("pom.xml")) != pom {
			t.Fatal("outside canary changed")
		}
	}
}

func TestWorkflowInputBoundary_ClassifiedReadAndParseFailures(t *testing.T) {
	t.Parallel()

	for _, malformed := range []bool{false, true} {
		files := fstest.MapFS{}
		if malformed {
			files["workflows/bad.yml"] = &fstest.MapFile{Data: []byte("jobs: [unterminated")}
		}

		var out bytes.Buffer

		err := appvalidate.JobGraph(&out, output.Annotator{}, appvalidate.JobGraphInput{FS: files, Workflows: []string{"workflows/bad.yml"}})

		want := errs.ErrMissingInput
		if malformed {
			want = errs.ErrMalformedInput
		}

		if !errors.Is(err, want) || out.Len() != 0 {
			t.Fatalf("jobgraph malformed=%v err=%v out=%s", malformed, err, &out)
		}

		err = appvalidate.Isolation(&out, output.Annotator{}, appvalidate.IsolationInput{FS: files, Workflow: "workflows/bad.yml", BuildJob: "build"})
		if !errors.Is(err, want) || out.Len() != 0 {
			t.Fatalf("isolation malformed=%v err=%v out=%s", malformed, err, &out)
		}

		if malformed {
			err = appvalidate.WorkflowInputDefaults(&out, output.Annotator{}, appvalidate.WorkflowInputDefaultsInput{FS: files, WorkflowsDir: "workflows"})
			if !errors.Is(err, errs.ErrMalformedInput) || out.Len() != 0 {
				t.Fatalf("defaults err=%v out=%s", err, &out)
			}
		}
	}

	root := t.TempDir()

	missing := filepath.Join(root, "missing.yml")
	if err := appvalidate.JobGraph(io.Discard, output.Annotator{}, appvalidate.JobGraphInput{Root: root, Workflows: []string{missing}}); !errors.Is(err, os.ErrNotExist) || !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("OS cause/class lost: %v", err)
	}
}

type unreadableWorkflowFS struct{}

func (unreadableWorkflowFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}
func TestWorkflowInputBoundary_UnreadableInput(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	for _, call := range []func() error{
		func() error {
			return appvalidate.JobGraph(&out, output.Annotator{}, appvalidate.JobGraphInput{FS: unreadableWorkflowFS{}, Workflows: []string{"file.yml"}})
		},
		func() error {
			return appvalidate.Isolation(&out, output.Annotator{}, appvalidate.IsolationInput{FS: unreadableWorkflowFS{}, Workflow: "file.yml"})
		},
		func() error {
			return appvalidate.WorkflowInputDefaults(&out, output.Annotator{}, appvalidate.WorkflowInputDefaultsInput{FS: unreadableWorkflowFS{}, WorkflowsDir: "workflows"})
		},
	} {
		if err := call(); !errors.Is(err, errs.ErrPermissionDenied) || !errors.Is(err, fs.ErrPermission) || out.Len() != 0 {
			t.Fatalf("unreadable input err=%v output=%s", err, &out)
		}
	}
}
