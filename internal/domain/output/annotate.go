// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// Annotator emits CI-platform annotations (error / warning / notice)
// to a writer in the encoding appropriate for the active output format.
//
// GitHub Actions consumes workflow commands of the shape
// `::error::msg\n` and surfaces them in the run-summary "Annotations"
// pane. Other formats (text / json / gitlab) get a `Error: msg` style
// prefix so the message stays human-readable in logs that don't parse
// the workflow command vocabulary.
//
// Annotator is a small value type — like *log.Logger but flatter.
// Callers construct one per CLI Action via NewAnnotator(stderr, format)
// and pass it to the app function that needs to emit annotations.
// No state, no resources, no Close.
type Annotator struct {
	w      io.Writer
	format Format
}

// NewAnnotator returns an Annotator that writes to w and emits in the
// shape required by format. Zero-value Annotator is safe to call (writes
// go to a discarded stream — useful as a default when annotations aren't
// configured).
func NewAnnotator(w io.Writer, f Format) Annotator {
	return Annotator{w: w, format: f}
}

// AnnotatorFromFlag is the common CLI-side one-liner: parse the raw
// `--output` flag value (case-insensitive, FormatAuto resolved against
// the active runner) and return an Annotator writing to w. Format
// parse failures fall through to FormatText since the root Before hook
// is responsible for rejecting bogus values before we reach here.
func AnnotatorFromFlag(w io.Writer, formatRaw string, runner provider.RunnerKind) Annotator {
	f, _ := ParseAndResolve(formatRaw, runner)

	return NewAnnotator(w, f)
}

// Annotation carries the optional GitHub workflow-command properties that
// scope an annotation to a source location or give it a title. A zero
// Annotation (all fields empty) renders a bare message-only annotation, so
// ErrorAt/WarningAt with Annotation{} are equivalent to Errorf/Warningf.
type Annotation struct {
	File  string // file=… — surfaces the annotation inline on that file
	Line  int    // line=… — only emitted when > 0
	Title string // title=… — annotation heading
}

// Errorf emits an error annotation. On GitHub Actions this is
// `::error::<msg>\n` (surfaces in the Annotations pane); elsewhere
// it's `Error: <msg>\n` for readable logs.
func (a Annotator) Errorf(msgFormat string, args ...any) {
	a.emit("error", "Error: ", msgFormat, args...)
}

// ErrorAt emits an error annotation scoped to a source location and/or title.
// On GitHub Actions it renders the full `::error file=…,line=…,title=…::<msg>`
// workflow command with every interpolated value escaped — so a hostile file
// path or message can't forge additional workflow commands. On other runners
// it renders a plain, readable `Error: <file>:<line>: <msg>` with no
// workflow-command noise.
func (a Annotator) ErrorAt(loc Annotation, msgFormat string, args ...any) {
	a.emitAt("error", "Error: ", loc, msgFormat, args...)
}

// Warningf emits a warning annotation. GHA: `::warning::<msg>`,
// elsewhere: `Warning: <msg>`.
func (a Annotator) Warningf(msgFormat string, args ...any) {
	a.emit("warning", "Warning: ", msgFormat, args...)
}

// WarningAt is Warningf's location/title-scoped sibling — see ErrorAt.
func (a Annotator) WarningAt(loc Annotation, msgFormat string, args ...any) {
	a.emitAt("warning", "Warning: ", loc, msgFormat, args...)
}

// Noticef emits a notice annotation. GHA: `::notice::<msg>`,
// elsewhere: `Notice: <msg>`.
func (a Annotator) Noticef(msgFormat string, args ...any) {
	a.emit("notice", "Notice: ", msgFormat, args...)
}

// emit writes one annotation line, picking the encoding per format.
// FormatGitHub uses the workflow command vocabulary; all other formats
// fall through to the human prefix because none of them have a
// universally-understood inline equivalent.
func (a Annotator) emit(ghaLevel, plainPrefix, msgFormat string, args ...any) {
	if a.w == nil {
		return // zero-value sink: discard
	}

	msg := fmt.Sprintf(msgFormat, args...)
	if a.format == FormatGitHub {
		_, _ = fmt.Fprintf(a.w, "::%s::%s\n", ghaLevel, EscapeWorkflowCommandData(msg))

		return
	}

	_, _ = fmt.Fprintf(a.w, "%s%s\n", plainPrefix, msg)
}

// emitAt is emit with optional file/line/title properties. GitHub gets the
// full workflow command with escaped properties and data; other formats get a
// readable location-prefixed line and never see a `::level::` token.
func (a Annotator) emitAt(ghaLevel, plainPrefix string, loc Annotation, msgFormat string, args ...any) {
	if a.w == nil {
		return // zero-value sink: discard
	}

	msg := fmt.Sprintf(msgFormat, args...)

	if a.format != FormatGitHub {
		_, _ = fmt.Fprintf(a.w, "%s%s%s\n", plainPrefix, plainLocationPrefix(loc), msg)

		return
	}

	_, _ = fmt.Fprintf(a.w, "::%s%s::%s\n", ghaLevel, ghaProperties(loc), EscapeWorkflowCommandData(msg))
}

// ghaProperties renders the leading " file=…,line=…,title=…" property block
// (with a leading space when non-empty), omitting unset fields and escaping
// each value with the stricter property encoding.
func ghaProperties(loc Annotation) string {
	var parts []string

	if loc.File != "" {
		parts = append(parts, "file="+EscapeWorkflowCommandProperty(loc.File))
	}

	if loc.Line > 0 {
		parts = append(parts, fmt.Sprintf("line=%d", loc.Line))
	}

	if loc.Title != "" {
		parts = append(parts, "title="+EscapeWorkflowCommandProperty(loc.Title))
	}

	if len(parts) == 0 {
		return ""
	}

	return " " + strings.Join(parts, ",")
}

// plainLocationPrefix renders "<file>:<line>: " for the non-GitHub fallback,
// omitting parts that are unset.
func plainLocationPrefix(loc Annotation) string {
	switch {
	case loc.File != "" && loc.Line > 0:
		return fmt.Sprintf("%s:%d: ", loc.File, loc.Line)
	case loc.File != "":
		return loc.File + ": "
	default:
		return ""
	}
}

// EscapeWorkflowCommandData encodes the *message* portion of a GitHub Actions
// workflow command so embedded control characters in interpolated/untrusted
// content can't forge a second command (annotation injection). Callers that
// hand-build `::error::`/`::warning::` lines with dynamic content must wrap that
// content with this (or EscapeWorkflowCommandProperty for property values).
func EscapeWorkflowCommandData(value string) string {
	return strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
	).Replace(value)
}

// EscapeWorkflowCommandProperty encodes a property *value* (e.g. `file=`,
// `title=`). GitHub requires the data escapes plus ":" and "," so a value can't
// terminate the property list or the command header.
func EscapeWorkflowCommandProperty(value string) string {
	return strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
		":", "%3A",
		",", "%2C",
	).Replace(value)
}
