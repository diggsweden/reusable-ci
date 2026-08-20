// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
	"gopkg.in/yaml.v3"
)

// IsolationInput drives Isolation.
type IsolationInput struct {
	// Workflow is the path to the workflow file to check (relative to FS when
	// FS is set, else an OS path).
	Workflow string
	// BuildJob is the artifact-producing job that must not see signing secrets.
	BuildJob string
	// SignJob is the cross-repo signing workflow call-site job. Empty keeps only
	// the generic core L3 checks enabled.
	SignJob string
	// PrepareJob is the job that produces release identity outputs and may run
	// fail-fast secret presence checks before checkout.
	PrepareJob string
	// DistDigestOutput is the build job output that feeds the signer's dist-digest
	// input.
	DistDigestOutput string
	// SigningSecrets are the secret names forbidden in BuildJob.
	SigningSecrets []string
	// SinglePinSubject enables directory-wide pin consistency for uses refs that
	// contain this subject (for forgejo-ci: "itiquette/forgejo-ci"). Empty disables
	// the check.
	SinglePinSubject string
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
		BuildJob:         in.BuildJob,
		SignJob:          in.SignJob,
		PrepareJob:       in.PrepareJob,
		DistDigestOutput: in.DistDigestOutput,
		SigningSecrets:   in.SigningSecrets,
	})
	if err != nil {
		return fmt.Errorf("check isolation %s: %w", in.Workflow, err)
	}

	violations = append(violations, singlePinViolations(in)...)
	violations = append(violations, siblingContractViolations(in, data)...)

	for _, v := range violations {
		annot.ErrorAt(output.Annotation{File: in.Workflow, Line: v.Line}, "%s", v.Msg)
	}

	if len(violations) > 0 {
		return fmt.Errorf("isolation validation failed for %s: %w", in.Workflow, errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(out, "%s SLSA Build L3 isolation invariants hold in %s\n", clicolor.Check(out), in.Workflow)

	return nil
}

func singlePinViolations(in IsolationInput) []validate.IsolationViolation {
	if in.SinglePinSubject == "" {
		return nil
	}

	files, err := workflowDirYAMLFiles(in)
	if err != nil {
		return []validate.IsolationViolation{{Line: 1, Msg: err.Error()}}
	}

	subjectRe := regexp.MustCompile(regexp.QuoteMeta(in.SinglePinSubject) + `[^ "'\n]*@[0-9a-f]{40}`)
	// The helpers input is named after the subject's repository, not after this
	// project. Hardcoding "forgejo-ci-helpers-sha" meant a consumer of any other
	// subject had that half of the check silently skipped: its helpers pin could
	// disagree with its uses: pins and nothing said so.
	helpersKey := in.SinglePinSubject
	if i := strings.LastIndex(helpersKey, "/"); i >= 0 {
		helpersKey = helpersKey[i+1:]
	}

	helpersRe := regexp.MustCompile(regexp.QuoteMeta(helpersKey) + `-helpers-sha:[[:space:]]*[0-9a-f]{40}`)
	shaRe := regexp.MustCompile(`[0-9a-f]{40}`)
	shas := map[string]struct{}{}

	for _, body := range files {
		for _, m := range subjectRe.FindAllString(body, -1) {
			if sha := shaRe.FindString(m); sha != "" {
				shas[sha] = struct{}{}
			}
		}

		for _, m := range helpersRe.FindAllString(body, -1) {
			if sha := shaRe.FindString(m); sha != "" {
				shas[sha] = struct{}{}
			}
		}
	}

	// A subject that matches nothing is not a workflow directory with one
	// consistent pin -- it is a check that ran over nothing and said so in the
	// same words. A stale or misspelled subject silently disabled this entirely.
	if len(shas) == 0 {
		return []validate.IsolationViolation{{
			Line: 1,
			Msg: fmt.Sprintf(
				"no %s pin found under workflow directory: the single-pin subject matches nothing, so pin consistency was not checked",
				in.SinglePinSubject),
		}}
	}

	if len(shas) == 1 {
		return nil
	}

	vals := make([]string, 0, len(shas))
	for sha := range shas {
		vals = append(vals, sha)
	}

	sort.Strings(vals)

	return []validate.IsolationViolation{{
		Line: 1,
		Msg:  fmt.Sprintf("mixed %s pins under workflow directory: %s", in.SinglePinSubject, strings.Join(vals, ", ")),
	}}
}

func siblingContractViolations(in IsolationInput, workflowYAML []byte) []validate.IsolationViolation {
	if in.SignJob == "" {
		return nil
	}

	uses := signJobUses(workflowYAML, in.SignJob)
	if uses == "" {
		return nil
	}

	siblingPath := siblingWorkflowPath(in.Workflow, uses)
	if siblingPath == "" {
		return nil
	}

	body, ok, err := readOptionalIsolationFile(in, siblingPath)
	if err != nil {
		return []validate.IsolationViolation{{Line: 1, Msg: err.Error()}}
	}

	if !ok {
		return nil
	}

	text := string(body)

	violations := missingContractSecrets(text, in.SigningSecrets)

	if !signerConsumesDistDigest(text) {
		violations = append(violations, validate.IsolationViolation{Line: 1, Msg: "sibling signer workflow does not consume inputs.dist-digest"})
	}

	if !signerVerifiesDistDigest(text) {
		violations = append(violations, validate.IsolationViolation{Line: 1, Msg: "sibling signer workflow does not appear to verify dist-digest before signing"})
	}

	return violations
}

// missingContractSecrets reports enforced signing secrets absent from the
// sibling signer's secrets-contract header (when such a header is declared).
func missingContractSecrets(body string, signingSecrets []string) []validate.IsolationViolation {
	declared := secretsContractHeader(body)
	if len(declared) == 0 {
		return nil
	}

	var violations []validate.IsolationViolation

	for _, secret := range signingSecrets {
		if secret == "" {
			continue
		}

		if _, ok := declared[secret]; !ok {
			violations = append(violations, validate.IsolationViolation{
				Line: 1,
				Msg:  fmt.Sprintf("gate enforces %s but sibling signer secrets-contract header does not list it", secret),
			})
		}
	}

	return violations
}

// signerConsumesDistDigest reports whether the signer workflow references the
// dist-digest input in any of the supported expression spellings.
func signerConsumesDistDigest(text string) bool {
	return strings.Contains(text, "inputs.dist-digest") ||
		strings.Contains(text, "inputs['dist-digest']") ||
		strings.Contains(text, `inputs["dist-digest"]`)
}

func workflowDirYAMLFiles(in IsolationInput) ([]string, error) {
	dir := workflowDir(in.Workflow)
	if in.FS != nil {
		entries, err := fs.ReadDir(in.FS, dir)
		if err != nil {
			return nil, fmt.Errorf("read workflow directory %s: %w", dir, err)
		}

		return readYAMLFilesFS(in.FS, dir, entries)
	}

	entries, err := os.ReadDir(filepath.FromSlash(dir))
	if err != nil {
		return nil, fmt.Errorf("read workflow directory %s: %w", dir, err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isYAMLFile(entry.Name()) {
			continue
		}

		data, err := os.ReadFile(filepath.Join(filepath.FromSlash(dir), entry.Name())) //nolint:gosec // workflow directory under caller-provided repo path.
		if err != nil {
			return nil, fmt.Errorf("read workflow file %s: %w", entry.Name(), err)
		}

		files = append(files, string(data))
	}

	return files, nil
}

func readYAMLFilesFS(fsys fs.FS, dir string, entries []fs.DirEntry) ([]string, error) {
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isYAMLFile(entry.Name()) {
			continue
		}

		data, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read workflow file %s: %w", entry.Name(), err)
		}

		files = append(files, string(data))
	}

	return files, nil
}

func readOptionalIsolationFile(in IsolationInput, name string) ([]byte, bool, error) {
	if in.FS != nil {
		data, err := fs.ReadFile(in.FS, name)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, false, nil
			}

			return nil, false, fmt.Errorf("read sibling signer workflow %s: %w", name, err)
		}

		return data, true, nil
	}

	data, err := os.ReadFile(filepath.FromSlash(name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}

		return nil, false, fmt.Errorf("read sibling signer workflow %s: %w", name, err)
	}

	return data, true, nil
}

func signJobUses(workflowYAML []byte, signJob string) string {
	var doc struct {
		Jobs map[string]yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(workflowYAML, &doc); err != nil {
		return ""
	}

	job, ok := doc.Jobs[signJob]
	if !ok || job.Kind != yaml.MappingNode {
		return ""
	}

	for i := 0; i+1 < len(job.Content); i += 2 {
		if job.Content[i].Value == "uses" && job.Content[i+1].Kind == yaml.ScalarNode {
			return job.Content[i+1].Value
		}
	}

	return ""
}

func siblingWorkflowPath(workflow, uses string) string {
	refIdx := strings.LastIndex(uses, "@")
	if refIdx < 0 {
		return ""
	}

	pathPart := uses[:refIdx]
	if schemeIdx := strings.Index(pathPart, "://"); schemeIdx >= 0 {
		afterScheme := pathPart[schemeIdx+3:]

		slashIdx := strings.Index(afterScheme, "/")
		if slashIdx < 0 {
			return ""
		}

		pathPart = afterScheme[slashIdx+1:]
	}

	parts := strings.Split(pathPart, "/")
	if len(parts) < 3 {
		return ""
	}

	repo := parts[1]
	subpath := path.Join(parts[2:]...)
	repoRoot := path.Dir(path.Dir(workflowDir(workflow)))

	return path.Join(path.Dir(repoRoot), repo, subpath)
}

func workflowDir(workflow string) string {
	cleaned := filepath.ToSlash(strings.TrimPrefix(workflow, "./"))

	return path.Dir(cleaned)
}

func isYAMLFile(name string) bool {
	return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
}

func secretsContractHeader(body string) map[string]struct{} {
	for _, line := range strings.Split(body, "\n") {
		contract, ok := strings.CutPrefix(line, "# workflow-call-secrets-contract:")
		if !ok {
			continue
		}

		declared := map[string]struct{}{}
		for _, field := range strings.Fields(contract) {
			declared[field] = struct{}{}
		}

		return declared
	}

	return nil
}

func signerVerifiesDistDigest(body string) bool {
	return strings.Contains(body, "validate-dist") ||
		(strings.Contains(body, "EXPECTED_DIGEST") && strings.Contains(body, "dist-digest")) ||
		strings.Contains(body, "sha256sum.*sha256sum") ||
		(strings.Contains(body, "sha256sum") && strings.Count(body, "sha256sum") >= 2)
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
