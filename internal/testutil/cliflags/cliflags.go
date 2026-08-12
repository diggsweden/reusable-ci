// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package cliflags reads urfave/cli flag metadata that the library does not
// expose through an interface, for the guards that audit the CLI surface.
package cliflags

import (
	"reflect"
	"testing"

	urfavecli "github.com/urfave/cli/v3"
)

// Sources extracts the value-source chain from any urfave/cli flag type.
//
// Every concrete flag type embeds a FlagBase carrying a Sources field, but
// the Flag interface does not expose it, so reflection is the only way in.
//
// A flag whose Sources cannot be read fails the test rather than yielding an
// empty chain. The callers are guards that look for env vars wired in the
// wrong place; "I could not read this flag" and "this flag reads no env vars"
// are opposite answers, and silently returning the second for the first is
// how a guard reports compliance it never verified.
func Sources(t *testing.T, flag urfavecli.Flag) urfavecli.ValueSourceChain {
	t.Helper()

	val := reflect.ValueOf(flag)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}

	field := val.FieldByName("Sources")
	if !field.IsValid() {
		t.Fatalf("flag %v has no Sources field; cannot audit its value sources", flag.Names())
	}

	chain, ok := field.Interface().(urfavecli.ValueSourceChain)
	if !ok {
		t.Fatalf("flag %v Sources is not a ValueSourceChain; cannot audit its value sources", flag.Names())
	}

	return chain
}
