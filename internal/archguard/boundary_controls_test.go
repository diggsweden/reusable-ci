// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cargo"
	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/xcode"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

func TestCredentialBoundary_ImportIdentityAndReferences(t *testing.T) {
	t.Parallel()

	const path = "github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	for _, tc := range []struct {
		source string
		want   int
	}{
		{`package p; import rc "` + path + `"; var mint = rc.OperatorCredential`, 1},
		{`package p; import . "` + path + `"; var mint = OperatorCredential`, 1},
		{`package p; import rc "` + path + `"; import "reflect"; var mint = reflect.ValueOf(rc.OperatorCredential)`, 1},
		{`package p; import runcontext "` + path + `"; var _ runcontext.Credential; func f(runcontext struct{OperatorCredential func(string)any}) {runcontext.OperatorCredential("x")}`, 0},
		{`package p; import . "` + path + `"; var _ Credential; func f(){OperatorCredential:=func(string)any{return nil};OperatorCredential("x")}`, 0},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", tc.source, 0)
		require.NoError(t, err)
		require.Len(t, operatorCredentialReferences(file), tc.want)
	}
}

func TestLayerBoundary_UtilitiesAndPurePGP(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		from, target string
		rejected     bool
	}{{"app/validate", "adapters/openpgp", true}, {"app/validate", "pgp", false}, {"newhelper", "cli", true}, {"newhelper", "adapters/github", true}, {"newhelper", "app/release", true}, {"newhelper", "domain/errs", false}, {"testutil/example", "adapters/git", false}, {"livetest", "cli", false}} {
		path := filepath.Join(t.TempDir(), "fixture.go")
		require.NoError(t, os.WriteFile(path, []byte("package fixture\nimport _ \""+internalPrefix+tc.target+"\"\n"), 0o600))
		found, err := violationsIn(token.NewFileSet(), path, layerOf(tc.from), forbiddenEdges())
		require.NoError(t, err)
		require.Equal(t, tc.rejected, len(found) > 0, tc.from+" -> "+tc.target)
	}
}

func TestExecBoundary_MissingProgramsAreUnavailable(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	repo := &adaptergit.Repo{Dir: t.TempDir(), GitBin: missing}
	for _, call := range []func(context.Context) error{
		func(ctx context.Context) error {
			_, err := (&cargo.Adapter{Bin: missing}).Version(ctx)

			return err
		},
		func(ctx context.Context) error {
			_, err := (&xcode.Security{Bin: missing}).Run(ctx, "find-identity")

			return err
		},
		func(ctx context.Context) error {
			_, err := repo.HasStagedChanges(ctx)

			return err
		},
		func(ctx context.Context) error {
			_, err := repo.IsAncestor(ctx, "a", "b")

			return err
		},
	} {
		err := call(t.Context())
		require.ErrorIs(t, err, errs.ErrDependencyUnavailable)
		require.EqualValues(t, 69, errs.ExitCodeFromError(err))
	}
}
