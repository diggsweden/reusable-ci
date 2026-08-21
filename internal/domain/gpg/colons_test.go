// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/gpg"
)

const sampleColons = `sec:u:255:22:ABCDEF1234567890:1700000000:::u:::scESC:::+:::ed25519:::0:
fpr:::::::::FEEDFACEDEADBEEF1234567890ABCDEF12345678:
grp:::::::::1234567890ABCDEF1234567890ABCDEF12345678:
uid:u::::1700000000::HASH123::Reusable CI Test (test) <ci-test@example.invalid>::::::::::0:
ssb:u:255:22:0123456789ABCDEF:1700000000:::::s:::+:::ed25519::
fpr:::::::::AABBCCDDEEFF11223344556677889900AABBCCDD:
grp:::::::::FEDCBA0987654321FEDCBA0987654321FEDCBA09:`

func TestParseKeygrips(t *testing.T) {
	t.Parallel()

	grips := gpg.ParseKeygrips(sampleColons)
	want := []string{
		"1234567890ABCDEF1234567890ABCDEF12345678",
		"FEDCBA0987654321FEDCBA0987654321FEDCBA09",
	}

	if len(grips) != 2 {
		t.Fatalf("got %d grips, want 2: %v", len(grips), grips)
	}

	for i, g := range grips {
		if g != want[i] {
			t.Errorf("grips[%d] = %q, want %q", i, g, want[i])
		}
	}
}

func TestParseKeygrips_Empty(t *testing.T) {
	t.Parallel()

	if got := gpg.ParseKeygrips("no grips"); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// TestAgentAck pins the classification of gpg-connect-agent transcripts.
// The cases marked "exits 0" are the ones that made a failed
// PRESET_PASSPHRASE look like a success: gpg-connect-agent reports both
// through its exit status as OK, so only the transcript distinguishes them.
func TestAgentAck(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		transcript string
		wantOK     bool
		wantDetail string
	}{
		{"plain_ok", "OK", true, ""},
		{"ok_with_text", "OK closing connection", true, ""},
		{"err_line", "ERR 67108924 Not supported <GPG Agent>", false, "ERR 67108924 Not supported <GPG Agent>"},
		{
			// exits 0: the agent answered, and refused.
			"err_after_greeting",
			"OK Pleased to meet you\nERR 67108875 Invalid value <GPG Agent>",
			false,
			"ERR 67108875 Invalid value <GPG Agent>",
		},
		{
			// exits 0: gpg-connect-agent never reached an agent at all.
			"no_agent_running",
			"gpg-connect-agent: can't connect to the gpg-agent: File name too long\ngpg-connect-agent: error sending standard options: No agent running",
			false,
			"gpg-connect-agent: can't connect to the gpg-agent: File name too long",
		},
		{"empty", "", false, "no response from gpg-agent"},
		{"whitespace_only", "\n  \n", false, "no response from gpg-agent"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ok, detail := gpg.AgentAck(testCase.transcript)
			if ok != testCase.wantOK {
				t.Errorf("AgentAck(%q) ok = %v, want %v", testCase.transcript, ok, testCase.wantOK)
			}

			if detail != testCase.wantDetail {
				t.Errorf("AgentAck(%q) detail = %q, want %q", testCase.transcript, detail, testCase.wantDetail)
			}
		})
	}
}
