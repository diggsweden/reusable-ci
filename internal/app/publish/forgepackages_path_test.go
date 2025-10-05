// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/require"
)

func TestForgePackagesNPMPublish_RefusesLinkedTarballsBeforePublish(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"file", "directory", "parent"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			dir := filepath.Join(root, "packages")
			require.NoError(t, os.Mkdir(dir, 0o700))

			canary := filepath.Join(root, "outside.tgz")
			require.NoError(t, os.WriteFile(canary, []byte("canary"), 0o600))

			switch kind {
			case "file":
				require.NoError(t, os.Symlink(canary, filepath.Join(dir, "app.tgz")))
			case "directory":
				require.NoError(t, os.Mkdir(filepath.Join(dir, "app.tgz"), 0o700))
			case "parent":
				require.NoError(t, os.WriteFile(filepath.Join(dir, "app.tgz"), []byte("package"), 0o600))
				alias := filepath.Join(root, "alias")
				require.NoError(t, os.Symlink(dir, alias))
				dir = alias
			}

			ops := &fakeNPMPublish{}
			resolver := fakeNPMRegistryResolver{reg: provider.ForgeNPMRegistry{Registry: "https://registry.invalid/", Token: "synthetic"}}

			var log bytes.Buffer

			err := apppublish.ForgePackagesNPMPublish(t.Context(), ops, resolver, &log, &log, apppublish.ForgePackagesNPMPublishInput{WorkingDir: dir})
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Empty(t, ops.args)
			require.Empty(t, log.String())

			body, err := os.ReadFile(canary)
			require.NoError(t, err)
			require.Equal(t, "canary", string(body))
		})
	}
}
