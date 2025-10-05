// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

func TestHostPin_RequiresEveryScannedFingerprintBeforeInstall(t *testing.T) {
	t.Parallel()

	const matching = "256 SHA256:fixture host (ED25519)"
	for _, line := range []string{"", matching, matching + "\n" + matching, matching + "\n256 SHA256:other host (ED25519)", matching + "\nbroken"} {
		root := t.TempDir()
		calls := 0
		run := func(_ context.Context, command string, args ...string) (string, error) {
			calls++

			if command == "ssh-keyscan" {
				require.Equal(t, []string{"-t", "ed25519", "host.invalid"}, args)

				return "host.invalid ssh-ed25519 synthetic-key", nil
			}

			require.Equal(t, "ssh-keygen", command)
			require.Equal(t, []string{"-lf", filepath.Join(root, "known_hosts.tmp")}, args)

			return line, nil
		}

		path, err := pinScannedHostKey(t.Context(), root, "host.invalid", 22, "ed25519", "SHA256:fixture", run)
		if line == matching || line == matching+"\n"+matching {
			require.NoError(t, err)

			body, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			require.Equal(t, "host.invalid ssh-ed25519 synthetic-key\n", string(body))
		} else {
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Empty(t, path)

			_, statErr := os.Stat(filepath.Join(root, "known_hosts"))
			require.ErrorIs(t, statErr, os.ErrNotExist)
		}

		require.Equal(t, 2, calls)
	}
}
