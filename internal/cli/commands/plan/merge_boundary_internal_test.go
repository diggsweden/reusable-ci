// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package plan

import (
	"context"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"os"
	"path/filepath"
	"testing"
)

func TestPlanMergeBoundary_RejectsNullAndPreservesNumbers(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{"container build":null}`, `{} {}`, `{"container build":{"large":9007199254740993,"exponent":1.2300e+4}}`} {
		t.Run(body, func(t *testing.T) {
			testenv.New(t)
			path := filepath.Join(t.TempDir(), "plan.json")
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
			sink := fakeoutputsink.New(t)
			writer := writeCmd()
			writer.Action = func(ctx context.Context, cmd *cli.Command) error {
				return runPlanWrite(ctx, cmd, &deps.Deps{OutputSink: sink})
			}
			root := &cli.Command{Name: "ci", Commands: []*cli.Command{writer, {Name: "container", Commands: []*cli.Command{{Name: "build", Flags: []cli.Flag{&cli.StringFlag{Name: "context"}}}}}}}

			var err error

			require.NotPanics(t, func() {
				err = root.Run(t.Context(), []string{"ci", "write", "--scope", "  container\t build  ", "--plan-file", path, "--set", "context=first", "--set", "context=last"})
			})

			data, readErr := os.ReadFile(path)
			require.NoError(t, readErr)

			if body[0] != '{' || body == `{"container build":null}` || body == `{} {}` {
				require.ErrorIs(t, err, errs.ErrMalformedInput)
				require.Equal(t, body, string(data))
				require.Empty(t, sink.Keys())
			} else {
				require.NoError(t, err)
				require.Contains(t, string(data), "9007199254740993")
				require.Contains(t, string(data), "1.2300e+4")
				parsed, err := planfile.Decode(data)
				require.NoError(t, err)
				require.Len(t, parsed, 1)
				require.Equal(t, "last", parsed["container build"]["context"])
			}
		})
	}
}
