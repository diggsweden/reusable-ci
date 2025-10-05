// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"testing"

	domainconfig "github.com/diggsweden/reusable-ci/v3/internal/domain/config"
)

// The schema tests in this file check what the schema accepts. The parser tests
// in internal/domain/config check what the parser accepts. Each is thorough and
// neither can see the case that matters most to an adopter: the two disagreeing.
//
// A disagreement is not a crash, it is a contradiction. Their editor underlines
// a file the tool then builds happily, or says a file is fine that the tool
// refuses at the top of a release run. Both directions cost the adopter the same
// thing — trust in whichever of the two they were reading.
//
// So every fixture here goes through BOTH, and the expectation is stated once.
// Where they are supposed to differ, the row says so and why, which is how the
// open `config:` block is recorded rather than silently tolerated.
//
// "The runtime" here means Parse followed by Validate, which is what
// appconfig.Validate does and therefore what an adopter's file is actually
// judged by. Parse alone is a pure decode — it accepts `project-type: cobol`
// happily — because the value rules live in Validate. Comparing the schema
// against Parse alone would report three disagreements that are really one
// split in the runtime, not a contract difference.

type agreementExpectation int

const (
	bothAccept agreementExpectation = iota
	bothReject
	// schemaAcceptsParserRejects is the declared asymmetry. The schema is
	// structural: it leaves per-ecosystem `config:` open because its shape
	// depends on project-type, and it does not encode cross-field rules
	// (signing combinations, a target's project and build type). The runtime
	// closes both.
	schemaAcceptsParserRejects
)

func TestSchemaAndParserAgree(t *testing.T) {
	t.Parallel()

	const base = "artifacts:\n  - name: app\n    project-type: maven\n"

	type row struct {
		name string
		body string
		want agreementExpectation
		why  string
	}

	stated := []row{
		{
			name: "the minimal valid file", body: base, want: bothAccept,
			why: "the shape every adopter starts from",
		},
		{
			name: "every top-level block", want: bothAccept,
			body: base + "containers:\n  - name: app\nsign:\n  method: sigstore\ngit-signing:\n  method: ssh\n",
			why:  "the blocks the schema declares must all parse",
		},
		{
			name: "an unknown top-level key", want: bothReject,
			body: base + "unknown-block: true\n",
			why:  "a typo in a block name",
		},
		{
			name: "an unknown artifact key", want: bothReject,
			body: "artifacts:\n  - name: app\n    project-type: maven\n    unknown-key: true\n",
			why:  "the most common adopter mistake",
		},
		{
			name: "an unknown container key", want: bothReject,
			body: base + "containers:\n  - name: app\n    unknown-key: true\n",
			why:  "containers are closed on both sides too",
		},
		{
			name: "an unknown sign key", want: bothReject,
			body: base + "sign:\n  method: sigstore\n  unknown-key: true\n",
			why:  "signing configuration is the last place to accept a typo quietly",
		},
		{
			name: "a project type that does not exist", want: bothReject,
			body: "artifacts:\n  - name: app\n    project-type: cobol\n",
			why:  "the enum is rendered from the Go type list, so both sides know it",
		},
		{
			name: "a sign method that does not exist", want: bothReject,
			body: base + "sign:\n  method: notary\n",
			why:  "same rendered-enum path",
		},
		{
			name: "a missing required artifact field", want: bothReject,
			body: "artifacts:\n  - project-type: maven\n",
			why:  "name has no omitempty and is in the schema's required list",
		},

		{
			name: "an unknown key inside the ecosystem config block", want: schemaAcceptsParserRejects,
			body: "artifacts:\n  - name: app\n    project-type: maven\n    config:\n      no-such-option: \"21\"\n",
			why: "DECLARED ASYMMETRY. The common instance is a misspelling of a real option. The schema " +
				"cannot constrain `config:` without a per-project-type conditional the renderer does not " +
				"emit, so an editor will not flag it. The parser does, with KnownFields, so the adopter " +
				"gets a clear error at the top of the run rather than a silently ignored setting. Losing " +
				"the parser half is the change that would hurt.",
		},
		{name: "python, the reserved project type", want: bothReject, body: "artifacts:\n  - name: app\n    project-type: python\n", why: "reserved until build and publish support exist"},
		{name: "auto as a project type", want: bothReject, body: "artifacts:\n  - name: app\n    project-type: auto\n", why: "auto is a detection mode, not a declared type"},
		{name: "an unknown build type", want: bothReject, body: base + "    build-type: bogus\n", why: "rendered build-type enum"},
		{name: "an unknown SBOM token", want: bothReject, body: base + "    sboms: build,bogus\n", why: "rendered SBOM pattern"},
		{name: "an unknown git-signing method", want: bothReject, body: base + "git-signing:\n  method: x509\n", why: "rendered git-signing enum"},
		{name: "kms without a public log", want: bothAccept, body: base + "sign:\n  method: kms\n  key: awskms:///alias/release\n  transparency: none\n", why: "the one method that may sign without Rekor"},
		{name: "npmjs on an npm artifact", want: schemaAcceptsParserRejects, body: "artifacts:\n  - name: app\n    project-type: npm\n    publish-to: [npmjs]\n", why: "DECLARED ASYMMETRY. npmjs is a reserved target name; which project types a target supports is a runtime rule."},
		{name: "maven-central for an application", want: schemaAcceptsParserRejects, body: base + "    build-type: application\n    publish-to: [maven-central]\n", why: "DECLARED ASYMMETRY. A target's build-type requirement is a cross-field rule."},
	}

	// The reviewed supported set, written out rather than read from the Go
	// table the schema enum is rendered from: swapping a type in that table
	// moves the schema and the parser together, so only a literal list notices.
	supported := []string{"maven", "npm", "gradle", "gradle-android", "xcode-ios", "go", "cargo", "meta"}
	signing := map[string]string{
		"kms without a key":           "  method: kms\n",
		"sigstore with a key":         "  method: sigstore\n  key: awskms:///alias/release\n",
		"gpg with an issuer":          "  method: gpg\n  oidc-issuer: https://issuer.example\n",
		"gpg with transparency":       "  method: gpg\n  transparency: none\n",
		"sigstore without public log": "  method: sigstore\n  transparency: none\n",
	}
	rows := append(make([]row, 0, len(stated)+len(supported)+len(signing)), stated...)

	for _, projectType := range supported {
		rows = append(rows, row{name: "supported project type " + projectType, want: bothAccept, body: "artifacts:\n  - name: app\n    project-type: " + projectType + "\n", why: "the reviewed supported set"})
	}

	// Signing combinations the runtime forbids and the structural schema allows.
	for name, sign := range signing {
		rows = append(rows, row{name: "signing " + name, want: schemaAcceptsParserRejects, body: base + "sign:\n" + sign, why: "DECLARED ASYMMETRY. Signing combinations are cross-field rules."})
	}

	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			schemaErr := loadSchema(t).Validate(yamlToInstance(t, []byte(tc.body)))
			parserErr := parseAndValidate([]byte(tc.body))

			switch tc.want {
			case bothAccept:
				if schemaErr != nil {
					t.Errorf("the schema rejects a file the tool accepts: %v\n%s\n%s", schemaErr, tc.why, tc.body)
				}

				if parserErr != nil {
					t.Errorf("the parser rejects a file the schema accepts: %v\n%s\n%s", parserErr, tc.why, tc.body)
				}
			case bothReject:
				if schemaErr == nil {
					t.Errorf("the schema accepts what the parser refuses; an adopter's editor would say this is fine\n%s\n%s", tc.why, tc.body)
				}

				if parserErr == nil {
					t.Errorf("the parser accepts what the schema refuses; the setting would be read despite the editor's warning\n%s\n%s", tc.why, tc.body)
				}
			case schemaAcceptsParserRejects:
				if schemaErr != nil {
					t.Errorf("the schema now constrains this; if that is deliberate, move the row to bothReject: %v", schemaErr)
				}

				if parserErr == nil {
					t.Errorf("the parser no longer refuses this, so nothing catches the typo at all\n%s\n%s", tc.why, tc.body)
				}
			}
		})
	}
}

// parseAndValidate is what the tool does to an adopter's file: decode it, then
// apply the value rules. appconfig.Validate is the same pair behind a path.
func parseAndValidate(body []byte) error {
	cfg, err := domainconfig.Parse(body)
	if err != nil {
		return err
	}

	return domainconfig.Validate(cfg)
}
