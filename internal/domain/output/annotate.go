// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

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
// the active platform) and return an Annotator writing to w. Format
// parse failures fall through to FormatText since the root Before hook
// is responsible for rejecting bogus values before we reach here.
func AnnotatorFromFlag(w io.Writer, formatRaw string, plat provider.Platform) Annotator {
	f, _ := ParseAndResolve(formatRaw, plat)

	return NewAnnotator(w, f)
}

// Errorf emits an error annotation. On GitHub Actions this is
// `::error::<msg>\n` (surfaces in the Annotations pane); elsewhere
// it's `Error: <msg>\n` for readable logs.
func (a Annotator) Errorf(msgFormat string, args ...any) {
	a.emit("error", "Error: ", msgFormat, args...)
}

// Warningf emits a warning annotation. GHA: `::warning::<msg>`,
// elsewhere: `Warning: <msg>`.
func (a Annotator) Warningf(msgFormat string, args ...any) {
	a.emit("warning", "Warning: ", msgFormat, args...)
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
		_, _ = fmt.Fprintf(a.w, "::%s::%s\n", ghaLevel, escapeGitHubWorkflowCommandData(msg))

		return
	}

	_, _ = fmt.Fprintf(a.w, "%s%s\n", plainPrefix, msg)
}

func escapeGitHubWorkflowCommandData(value string) string {
	replacer := strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
	)

	return replacer.Replace(value)
}
