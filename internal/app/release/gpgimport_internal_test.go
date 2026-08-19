// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"errors"
	"strings"
	"testing"
)

var errPreset = errors.New("agent refused")

type fakeGPGAgent struct {
	gripsText  string
	listErr    error
	agentErr   error
	presetErr  error
	configured int
	preset     []string
	passphrase string
}

func (f *fakeGPGAgent) ImportKey(context.Context, []byte) error { return nil }

func (f *fakeGPGAgent) ConfigureAgent(context.Context) error {
	f.configured++

	return f.agentErr
}

func (f *fakeGPGAgent) ListKeygrips(context.Context, string) (string, error) {
	return f.gripsText, f.listErr
}

func (f *fakeGPGAgent) PresetPassphrase(_ context.Context, keygrip, passphrase string) error {
	f.preset = append(f.preset, keygrip)
	f.passphrase = passphrase

	return f.presetErr
}

// colons is gpg --with-colons output for a primary key with a signing
// subkey — two keygrips, which is the ordinary shape for a release key.
const colons = `sec:u:255:22:ABC:::::::::
grp:::::::::AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA:
ssb:u:255:22:DEF:::::::::
grp:::::::::BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB:
`

// TestPresetPassphraseInAgent_PresetsEveryKeygrip covers a function with
// no coverage in either tier.
//
// A release key normally has a signing subkey, so gpg reports two
// keygrips. Presetting only the first leaves the subkey locked, and the
// signing step then blocks on a passphrase prompt that never comes —
// visible as a hung job rather than as an error.
func TestPresetPassphraseInAgent_PresetsEveryKeygrip(t *testing.T) {
	t.Parallel()

	agent := &fakeGPGAgent{gripsText: colons}

	if err := presetPassphraseInAgent(context.Background(), agent, "ABC", "s3cret"); err != nil {
		t.Fatal(err)
	}

	if agent.configured != 1 {
		t.Errorf("ConfigureAgent called %d times, want 1", agent.configured)
	}

	want := []string{
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
	}
	if len(agent.preset) != len(want) {
		t.Fatalf("preset %v, want both keygrips %v", agent.preset, want)
	}

	for i := range want {
		if agent.preset[i] != want[i] {
			t.Errorf("preset[%d] = %q, want %q", i, agent.preset[i], want[i])
		}
	}

	if agent.passphrase != "s3cret" {
		t.Errorf("passphrase = %q, want it passed through unchanged", agent.passphrase)
	}
}

// TestPresetPassphraseInAgent_Failures covers the error paths, and that
// none of them echoes the passphrase. The error surfaces in a CI log.
func TestPresetPassphraseInAgent_Failures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		agent *fakeGPGAgent
		want  string
	}{
		{
			name:  "agent cannot be configured",
			agent: &fakeGPGAgent{agentErr: errPreset},
			want:  "configure gpg-agent",
		},
		{
			name:  "keygrips cannot be listed",
			agent: &fakeGPGAgent{listErr: errPreset},
			want:  "list keygrips",
		},
		{
			// The message names the keygrip, which is public, rather than
			// the passphrase.
			name:  "the agent refuses a preset",
			agent: &fakeGPGAgent{gripsText: colons, presetErr: errPreset},
			want:  "preset passphrase for AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := presetPassphraseInAgent(context.Background(), tc.agent, "ABC", "s3cret")
			if err == nil {
				t.Fatal("expected an error")
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}

			if !errors.Is(err, errPreset) {
				t.Errorf("err = %v, want the underlying cause preserved", err)
			}

			if strings.Contains(err.Error(), "s3cret") {
				t.Errorf("error echoed the passphrase: %v", err)
			}
		})
	}
}
