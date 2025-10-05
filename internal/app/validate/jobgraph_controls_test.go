// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// jobGraphMaskingLine9 is the masking workflow pushed down one line, so each
// annotation's line is attributed to its own file rather than coinciding.
const jobGraphMaskingLine9 = "# pushed down\n" + jobGraphMasking

// annotationTargets returns the "file=…,line=…" property of every error
// annotation in order.
func annotationTargets(text string) []string {
	var targets []string

	for line := range strings.SplitSeq(text, "\n") {
		if rest, ok := strings.CutPrefix(line, "::error "); ok {
			target, _, _ := strings.Cut(rest, "::")
			targets = append(targets, target)
		}
	}

	return targets
}

// TestJobGraph_EveryFileIsCheckedWithItsOwnAttribution puts a dangerous file
// after another dangerous file and a clean one, in every way the files can be
// named: an explicit list in memory or on disk, with relative and absolute
// entries, and a scanned directory in memory or on disk under a root that is
// not the working directory. Every violation is reported with its own file and
// line, explicit lists in the order given, and scanned directories in path
// order with other extensions and subdirectories (even one named like a
// workflow) skipped.
func TestJobGraph_EveryFileIsCheckedWithItsOwnAttribution(t *testing.T) {
	seedMemory := func(t *testing.T, dir string) *testfs.Memory {
		t.Helper()

		mem := testfs.NewMemory(t)
		mem.WriteFile(dir+"/z.yaml", []byte(jobGraphMaskingLine9))
		mem.WriteFile(dir+"/a.yml", []byte(jobGraphMasking))
		mem.WriteFile(dir+"/m.yml", []byte(jobGraphClean))
		mem.WriteFile(dir+"/notes.md", []byte(jobGraphMasking))
		mem.WriteFile(dir+"/nested.yml/deep.yml", []byte(jobGraphMasking))

		return mem
	}

	seedDisk := func(t *testing.T, dir string) *testfs.Real {
		t.Helper()

		repo := testfs.NewReal(t)
		repo.WriteFile(dir+"/z.yaml", []byte(jobGraphMaskingLine9))
		repo.WriteFile(dir+"/a.yml", []byte(jobGraphMasking))
		repo.WriteFile(dir+"/m.yml", []byte(jobGraphClean))
		repo.WriteFile(dir+"/notes.md", []byte(jobGraphMasking))
		repo.WriteFile(dir+"/nested.yml/deep.yml", []byte(jobGraphMasking))

		elsewhere := testfs.NewReal(t)
		elsewhere.Chdir()

		return repo
	}

	t.Run("explicit list in memory", func(t *testing.T) {
		mem := seedMemory(t, ".forgejo/workflows")
		assertJobGraphTargets(t, appvalidate.JobGraphInput{
			Root:      ".",
			FS:        mem.FS(),
			Workflows: []string{"./.forgejo/workflows/z.yaml", ".forgejo/workflows/m.yml", ".forgejo/workflows/a.yml"},
		}, []string{"file=.forgejo/workflows/z.yaml,line=9", "file=.forgejo/workflows/a.yml,line=8"})
	})

	t.Run("scanned directory in memory", func(t *testing.T) {
		mem := seedMemory(t, "ci/flows")
		assertJobGraphTargets(t, appvalidate.JobGraphInput{Root: ".", FS: mem.FS(), WorkflowsDir: "ci/flows"},
			[]string{"file=ci/flows/a.yml,line=8", "file=ci/flows/z.yaml,line=9"})
	})

	t.Run("explicit list on disk", func(t *testing.T) {
		repo := seedDisk(t, ".github/workflows")
		absolute := repo.Path(".github/workflows/a.yml")
		assertJobGraphTargets(t, appvalidate.JobGraphInput{
			Root:      repo.Root,
			Workflows: []string{".github/workflows/z.yaml", ".github/workflows/m.yml", absolute},
		}, []string{"file=.github/workflows/z.yaml,line=9", "file=" + filepath.ToSlash(absolute) + ",line=8"})
	})

	t.Run("scanned default directory on disk", func(t *testing.T) {
		repo := seedDisk(t, ".github/workflows")
		assertJobGraphTargets(t, appvalidate.JobGraphInput{Root: repo.Root},
			[]string{"file=.github/workflows/a.yml,line=8", "file=.github/workflows/z.yaml,line=9"})
	})

	t.Run("scanned custom directory on disk", func(t *testing.T) {
		repo := seedDisk(t, "ci/flows")
		assertJobGraphTargets(t, appvalidate.JobGraphInput{Root: repo.Root, WorkflowsDir: repo.Path("ci/flows")},
			[]string{"file=ci/flows/a.yml,line=8", "file=ci/flows/z.yaml,line=9"})
	})
}

func assertJobGraphTargets(t *testing.T, in appvalidate.JobGraphInput, want []string) {
	t.Helper()

	var out, annot bytes.Buffer

	err := appvalidate.JobGraph(&out, output.NewAnnotator(&annot, output.FormatGitHub), in)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation\n%s", err, annot.String())
	}

	if got := annotationTargets(annot.String()); !slices.Equal(got, want) {
		t.Errorf("annotations = %q, want %q", got, want)
	}

	if out.Len() != 0 {
		t.Errorf("a failed check reported success: %q", out.String())
	}
}

// TestJobGraph_LateFailuresKeepTheirClass puts an unreadable or unparsable
// file after a dangerous one. The earlier violation is still reported, and the
// run stops with the later file's own class rather than passing or reading as
// an ordinary violation, because it no longer covers every requested workflow.
func TestJobGraph_LateFailuresKeepTheirClass(t *testing.T) {
	for _, tc := range []struct {
		name  string
		seed  func(t *testing.T, repo *testfs.Real)
		input func(repo *testfs.Real) appvalidate.JobGraphInput
		want  error
	}{
		{
			name: "explicit file missing",
			input: func(repo *testfs.Real) appvalidate.JobGraphInput {
				return appvalidate.JobGraphInput{Root: repo.Root, Workflows: []string{".github/workflows/a.yml", ".github/workflows/gone.yml"}}
			},
			want: errs.ErrMissingInput,
		},
		{
			name: "explicit file malformed",
			seed: func(_ *testing.T, repo *testfs.Real) {
				repo.WriteFile(".github/workflows/b.yml", []byte("jobs: [unterminated\n"))
			},
			input: func(repo *testfs.Real) appvalidate.JobGraphInput {
				return appvalidate.JobGraphInput{Root: repo.Root, Workflows: []string{".github/workflows/a.yml", ".github/workflows/b.yml"}}
			},
			want: errs.ErrMalformedInput,
		},
		{
			name: "scanned link to nothing",
			seed: func(t *testing.T, repo *testfs.Real) {
				t.Helper()

				if err := os.Symlink(repo.Path("nowhere.yml"), repo.Path(".github/workflows/b.yml")); err != nil {
					t.Fatal(err)
				}
			},
			input: func(repo *testfs.Real) appvalidate.JobGraphInput { return appvalidate.JobGraphInput{Root: repo.Root} },
			want:  errs.ErrMissingInput,
		},
		{
			name: "scanned file malformed",
			seed: func(_ *testing.T, repo *testfs.Real) {
				repo.WriteFile(".github/workflows/b.yml", []byte("jobs: [unterminated\n"))
			},
			input: func(repo *testfs.Real) appvalidate.JobGraphInput { return appvalidate.JobGraphInput{Root: repo.Root} },
			want:  errs.ErrMalformedInput,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := testfs.NewReal(t)
			repo.WriteFile(".github/workflows/a.yml", []byte(jobGraphMasking))

			if tc.seed != nil {
				tc.seed(t, repo)
			}

			var out, annot bytes.Buffer

			err := appvalidate.JobGraph(&out, output.NewAnnotator(&annot, output.FormatGitHub), tc.input(repo))
			if !errors.Is(err, tc.want) || errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want %v and not ErrValidation", err, tc.want)
			}

			if !strings.Contains(err.Error(), ".github/workflows/b.yml") && !strings.Contains(err.Error(), ".github/workflows/gone.yml") {
				t.Errorf("error does not name the failing file: %v", err)
			}

			if got := annotationTargets(annot.String()); !slices.Equal(got, []string{"file=.github/workflows/a.yml,line=8"}) {
				t.Errorf("annotations = %q, want the earlier file's violation", got)
			}

			if out.Len() != 0 {
				t.Errorf("an incomplete run reported success: %q", out.String())
			}
		})
	}
}

// TestWorkflowScans_DirectoryMustBeInsideTheRoot covers both directory-scanning
// validators. A directory outside the root, or a relative one that resolves
// outside it, is a usage error naming both, where it used to fail as an
// unclassified path computation or as malformed input about "..". A directory
// inside the root given from the working directory is scanned.
func TestWorkflowScans_DirectoryMustBeInsideTheRoot(t *testing.T) {
	parent := testfs.NewReal(t)
	parent.WriteFile("repo/ci/flows/a.yml", []byte(jobGraphMasking))
	parent.WriteFile("other/flows/a.yml", []byte(jobGraphMasking))
	parent.Chdir()

	scans := map[string]func(root, dir string) error{
		"job graph": func(root, dir string) error {
			return appvalidate.JobGraph(&bytes.Buffer{}, output.NewAnnotator(&bytes.Buffer{}, output.FormatGitHub), appvalidate.JobGraphInput{Root: root, WorkflowsDir: dir})
		},
		"input defaults": func(root, dir string) error {
			return appvalidate.WorkflowInputDefaults(&bytes.Buffer{}, output.NewAnnotator(&bytes.Buffer{}, output.FormatGitHub), appvalidate.WorkflowInputDefaultsInput{Root: root, WorkflowsDir: dir})
		},
	}

	for name, scan := range scans {
		for _, tc := range []struct {
			root, dir string
			want      error
		}{
			{parent.Path("repo"), parent.Path("other/flows"), errs.ErrUsage},
			{parent.Path("repo"), "other/flows", errs.ErrUsage},
			{"repo", "other/flows", errs.ErrUsage},
			{"repo", "repo/..", errs.ErrUsage},
			{parent.Path("repo"), "repo/ci/flows", nil},
			{"repo", parent.Path("repo/ci/flows"), nil},
		} {
			t.Run(name+"/"+tc.root+"|"+tc.dir, func(t *testing.T) {
				err := scan(tc.root, tc.dir)
				if tc.want == nil {
					if errors.Is(err, errs.ErrUsage) || errors.Is(err, errs.ErrMalformedInput) || errors.Is(err, errs.ErrMissingInput) {
						t.Fatalf("directory inside the root was not scanned: %v", err)
					}

					return
				}

				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
			})
		}
	}
}
