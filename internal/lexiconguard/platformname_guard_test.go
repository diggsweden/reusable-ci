// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package lexiconguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
)

// platformOwner is the one package entitled to a type called Platform.
//
// In this repository Platform means an OCI image platform: os/arch with an
// optional variant, as the OCI image spec defines it and as `--platform
// linux/amd64` spells it for buildah, skopeo and docker. That meaning is
// fixed by an external standard, so it owns the word.
const platformOwner = "internal/domain/container"

// TestPlatformTypeIsSingleSourced keeps the word Platform meaning one thing.
//
// The forge-API axis was also called Platform, so the repository had two
// exported types of that name meaning unrelated things -- os/arch in one
// package, "which forge's REST API" in another. Everything around the second
// one had already routed around it: the docs say forge, the doctor JSON emits
// forge_api, the composition root named its parameters forge, and the struct
// field carried a comment translating itself back to the type. It is now
// provider.ForgeAPI, paired with provider.RunnerKind as the two axes
// docs/providers.md describes.
//
// The rename fixed the collision once; this is what stops it recurring, since
// Platform is the obvious word to reach for and nothing in the compiler
// objects to a second one.
//
// Scope: exported type declarations only. Local variables, struct fields and
// parameters called platform are not policed -- inside the container tree
// they are usually correct, and outside it the type name is what misleads.
func TestPlatformTypeIsSingleSourced(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)

	var offenders []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "dist" || name == "node_modules" {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, platformOwner+"/") {
			return nil
		}

		names, parseErr := exportedTypesNamed(path, "Platform")
		if parseErr != nil {
			return parseErr
		}

		for range names {
			offenders = append(offenders, rel)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}

	if len(offenders) > 0 {
		t.Errorf(
			"type Platform declared outside %s: %s\n"+
				"    Platform means an OCI image platform (os/arch) in this repository, and\n"+
				"    %s owns it.\n"+
				"    If you mean which forge's API to talk to, that is provider.ForgeAPI.\n"+
				"    If you mean which runner's output conventions to emit, that is\n"+
				"    provider.RunnerKind. See docs/providers.md for the two axes.",
			platformOwner, strings.Join(offenders, ", "), platformOwner,
		)
	}
}

// exportedTypesNamed returns each exported type declaration in path whose name
// equals want.
func exportedTypesNamed(path, want string) ([]string, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	var found []string

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}

		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if ok && ts.Name.Name == want {
				found = append(found, ts.Name.Name)
			}
		}
	}

	return found, nil
}
