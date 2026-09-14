// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConfigPlanHasNoStandaloneEntryPoints keeps the validation boundary from
// growing a way around itself.
//
// A ConfigPlan crosses a process boundary as JSON, so every consumer decodes one
// and every decoder validates before acting. That only holds while the plan
// constructors are the way in. An exported helper here that takes a ConfigPlan
// is a second door: a caller can reach it with a plan nothing checked, and the
// helper will happily project fields out of it. The stage-plan builders were
// exactly that until they became internal to the constructors that validate.
//
// This is a structural check, not a behavioural one: it says which functions may
// accept a ConfigPlan, not what they do with a valid one.
func TestConfigPlanHasNoStandaloneEntryPoints(t *testing.T) {
	t.Parallel()

	// The functions whose whole job is the boundary itself.
	validating := map[string]string{
		"ValidateConfigPlan":     "is the validator",
		"NewReleasePlan":         "validates before building the plan",
		"NewSnapshotReleasePlan": "validates before building the plan",
	}

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	var offenders []string

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, parseErr := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		require.NoErrorf(t, parseErr, "parse %s", name)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() || !takesConfigPlan(fn) {
				continue
			}

			if _, allowed := validating[fn.Name.Name]; allowed {
				continue
			}

			offenders = append(offenders, filepath.Join(name, fn.Name.Name))
		}
	}

	require.Emptyf(t, offenders,
		"these exported functions accept a ConfigPlan without being one of the validating entry points (%v). "+
			"Either validate inside them, or make them internal to a constructor that does.", offenders)
}

// takesConfigPlan reports whether any parameter is a ConfigPlan, by value or by
// pointer. A plan reached through a struct field is a different shape and is not
// what this check is about.
func takesConfigPlan(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil {
		return false
	}

	for _, param := range fn.Type.Params.List {
		expr := param.Type
		if star, ok := expr.(*ast.StarExpr); ok {
			expr = star.X
		}

		if ident, ok := expr.(*ast.Ident); ok && ident.Name == "ConfigPlan" {
			return true
		}
	}

	return false
}
