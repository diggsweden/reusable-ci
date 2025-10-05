// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package planfile_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
)

// resolveFlag runs a one-flag command and returns what the flag resolved to.
func resolveFlag(t *testing.T, flag cli.Flag, args ...string) string {
	t.Helper()

	var got string

	cmd := &cli.Command{
		Name:  "test",
		Flags: []cli.Flag{flag},
		Action: func(_ context.Context, cmd *cli.Command) error {
			got = cmd.String("context")

			return nil
		},
	}
	require.NoError(t, cmd.Run(context.Background(), append([]string{"test"}, args...)))

	return got
}

func writePlan(t *testing.T, content string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "plan.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	t.Setenv(planfile.EnvVar, path)
}

func TestPlanPrecedence_FlagBeatsPlanBeatsEnvBeatsDefault(t *testing.T) {
	testenv.New(t)
	// Setenv registers restoration; an empty value is still present to cli.EnvVars.
	t.Setenv("BUILD_CONTEXT", "")
	require.NoError(t, os.Unsetenv("BUILD_CONTEXT"))

	newFlag := func() cli.Flag {
		return &cli.StringFlag{
			Name:    "context",
			Value:   "default-value",
			Sources: planfile.Vars("container build", "context", "BUILD_CONTEXT"),
		}
	}

	t.Run("flag beats plan and env", func(t *testing.T) {
		writePlan(t, `{"container build": {"context": "from-plan"}}`)
		t.Setenv("BUILD_CONTEXT", "from-env")
		require.Equal(t, "from-flag", resolveFlag(t, newFlag(), "--context", "from-flag"))
	})

	t.Run("plan beats env", func(t *testing.T) {
		writePlan(t, `{"container build": {"context": "from-plan"}}`)
		t.Setenv("BUILD_CONTEXT", "from-env")
		require.Equal(t, "from-plan", resolveFlag(t, newFlag()))
	})

	t.Run("env wins when plan lacks the key", func(t *testing.T) {
		writePlan(t, `{"container build": {"other": "x"}}`)
		t.Setenv("BUILD_CONTEXT", "from-env")
		require.Equal(t, "from-env", resolveFlag(t, newFlag()))
	})

	t.Run("default when nothing is set", func(t *testing.T) {
		require.Equal(t, "default-value", resolveFlag(t, newFlag()))
	})

	t.Run("scalar json values are stringified", func(t *testing.T) {
		writePlan(t, `{"container build": {"context": 42}}`)
		require.Equal(t, "42", resolveFlag(t, newFlag()))
	})

	t.Run("malformed plan is treated as absent", func(t *testing.T) {
		writePlan(t, `{not json`)
		t.Setenv("BUILD_CONTEXT", "from-env")
		require.Equal(t, "from-env", resolveFlag(t, newFlag()))
	})

	t.Run("structured value warns and falls through", func(t *testing.T) {
		writePlan(t, `{"container build": {"context": {"nested": true}}}`)
		t.Setenv("BUILD_CONTEXT", "from-env")
		require.Equal(t, "from-env", resolveFlag(t, newFlag()))
	})
}

// recordingHandler captures slog records so the warning can be inspected.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.records = append(h.records, r.Clone())

	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// warnings returns the messages and attributes of the recorded warnings.
func (h *recordingHandler) warnings() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	var out []string

	for _, r := range h.records {
		if r.Level != slog.LevelWarn {
			continue
		}

		line := r.Message
		r.Attrs(func(a slog.Attr) bool {
			line += " " + a.Key + "=" + a.Value.String()

			return true
		})

		out = append(out, line)
	}

	return out
}

// TestPlanFile_MalformedPlanWarnsOnceAndSafely makes the warning observable.
//
// A malformed plan is treated as absent so one bad file does not fail the whole
// command, and it warns so the operator learns why their plan had no effect.
// Neither half was asserted: the warning goes to the default slog logger, which
// no test installed, so "it warns" was a claim about a line nobody read.
//
// Two properties matter. It warns ONCE per path rather than once per flag —
// a plan feeds many flags, and a per-flag warning would bury the run in
// identical lines. And the warning names the path and the decode error only:
// the plan file is operator input that can carry values from a secret store, so
// echoing its contents into a log is the failure mode the once-guard would make
// louder rather than quieter.
func TestPlanFile_MalformedPlanWarnsOnceAndSafely(t *testing.T) {
	handler := &recordingHandler{}
	previous := slog.Default()

	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(previous) })

	env := testenv.New(t)
	dir := env.MkdirAll("planfile-warn")

	// A unique path: the once-per-path guard is a package global that
	// outlives any single test.
	path := filepath.Join(dir, "malformed-"+t.Name()+".json")

	//nolint:gosec // G101: a synthetic canary, not a credential.
	const secretish = "CANARY-PLAN-VALUE-3f81ad"

	require.NoError(t, os.WriteFile(path, []byte(`{"container build": {"context": "`+secretish+`"`), 0o600))
	t.Setenv(planfile.EnvVar, path)

	// Two different flags read the same plan, so a per-flag warning would
	// produce two lines.
	for _, key := range []string{"context", "containerfile"} {
		cmd := &cli.Command{
			Flags: []cli.Flag{&cli.StringFlag{
				Name:    key,
				Sources: planfile.Vars("container build", key, "UNSET_FOR_THIS_TEST"),
			}},
		}
		require.NoError(t, cmd.Run(context.Background(), []string{"x"}))
	}

	got := handler.warnings()
	if len(got) != 1 {
		t.Fatalf("warnings = %v, want exactly one for a malformed plan", got)
	}

	if !strings.Contains(got[0], path) {
		t.Errorf("warning does not name the plan path: %q", got[0])
	}

	if strings.Contains(got[0], secretish) {
		t.Errorf("the warning echoed the plan's contents: %q", got[0])
	}
}
