// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	appconfig "github.com/diggsweden/reusable-ci/v3/internal/app/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// TestEmitConfigPlan_SummaryIsExactOrderedAndLiteral compares the whole
// configuration summary for two artifacts and two containers in declaration
// order, each row associated with its own artifact or container (type,
// publish targets, directory, sources, derived artifact types, file), and an
// artifact name carrying Markdown and HTML that must arrive as literal text.
// artifacts.yml is pull-request-editable, and the name used to reach the job
// summary as Markdown.
func TestEmitConfigPlan_SummaryIsExactOrderedAndLiteral(t *testing.T) {
	t.Parallel()

	path := writeYAML(t, `
artifacts:
  - name: "api [docs](#pwn) <img src=x>"
    project-type: maven
    build-type: library
    working-directory: services/api
    publish-to: [maven-central, forge-packages]
  - name: web
    project-type: npm
containers:
  - name: "api-image *bold*"
    from: ["api [docs](#pwn) <img src=x>"]
    container-file: services/api/Containerfile
  - name: web-image
    from: [web]
`)

	summary := &recordingSummary{}
	require.NoError(t, appconfig.EmitConfigPlan(t.Context(), fakeoutputsink.New(t), summary, io.Discard, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path}))
	require.Len(t, summary.appended, 1)

	const hostile = "api &#91;docs&#93;(&#35;pwn) &#60;img src=x&#62;"

	require.Equal(t, "## Configuration\n"+
		"### "+hostile+"\n- **Type:** maven\n- **Publish To:** maven-central, forge-packages\n- **Directory:** services/api\n\n"+
		"### web\n- **Type:** npm\n- **Publish To:** \n- **Directory:** .\n\n"+
		"\n## Containers\n"+
		"### api-image &#42;bold&#42;\n- **From:** "+hostile+"\n- **Artifact Types:** maven\n- **Containerfile:** services/api/Containerfile\n\n"+
		"### web-image\n- **From:** web\n- **Artifact Types:** npm\n- **Containerfile:** Containerfile\n\n",
		summary.appended[0])
}
