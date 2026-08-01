// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

// EventContextInput drives `reusable-ci validate event-context`.
type EventContextInput struct {
	// EventName is GITHUB_EVENT_NAME (the workflow's trigger event).
	EventName string
	// AllowedEvents overrides DefaultAllowedEvents. Empty falls back to
	// the default policy. Comma/space/newline-separated lists from the
	// CLI flag are pre-split by the caller.
	AllowedEvents []string
}

// EventContext refuses to proceed when the workflow trigger is outside
// the allowed-events policy. The privileged publish / release workflows
// call this as their first runtime step so secrets in scope don't reach
// any downstream operation when the caller wired a `pull_request*`
// trigger by mistake (or by malice).
//
// On refuse:
//   - emits an error annotation via annot (GHA: `::error::`; else plain)
//   - returns a wrapped errs.ErrValidation
//
// On accept: prints a one-line confirmation to out.
func EventContext(out io.Writer, annot output.Annotator, in EventContextInput) error {
	err := validate.RequireAllowedEvent(in.EventName, in.AllowedEvents)
	if err == nil {
		_, _ = fmt.Fprintf(out, "%s Trigger event %q is allowed\n", clicolor.Check(out), in.EventName)

		return nil
	}

	var ece *validate.EventContextError
	if !errors.As(err, &ece) {
		// Empty / missing input — surface as-is.
		return err
	}

	guidance := guidanceFor(ece.Got)
	allowed := strings.Join(ece.Allowed, ", ")

	// On GitHub the Annotator surfaces this in the run-summary Annotations
	// pane (`::error title=…::`); elsewhere it's a plain `Error:` line. The
	// same content is also in the wrapped Go error below. ece.Error() embeds
	// the workflow-supplied event name, which the Annotator escapes.
	annot.ErrorAt(output.Annotation{Title: "Refused trigger event"}, "%s", ece.Error())

	return fmt.Errorf(
		"trigger event %q is not allowed to run privileged publish/release workflows\n"+
			"allowed events: %s\n"+
			"%s: %w",
		ece.Got, allowed, guidance, errs.ErrValidation,
	)
}

// guidanceFor returns event-specific remediation text. The PR-family
// path is the high-likelihood misconfig and gets a tailored message;
// other refusals fall through to a generic line.
func guidanceFor(eventName string) string {
	if validate.IsPullRequestEvent(eventName) {
		return "PR-context callers expose signing/package secrets to the PR HEAD. " +
			"Gate the caller workflow to push / workflow_dispatch / release / " +
			"schedule / workflow_run / merge_group, or — only for explicit preview " +
			"flows — pass --allowed-events on this step"
	}

	return "gate the caller workflow to one of the listed events, or pass " +
		"--allowed-events on this step to extend the policy"
}
