// Package uxtest provides test helpers for manipulating package-level
// globals in internal/ux. Tests that call ux.EnableQuiet, ux.DisableColor,
// or assign to color vars directly must save and restore these globals
// to avoid leaking state into sibling tests.
package uxtest

import (
	"testing"

	"github.com/jorge-barreto/orc/internal/ux"
)

// SaveState snapshots all ux globals that tests commonly mutate and
// registers a t.Cleanup to restore them. Call once at the top of a test
// that touches any ux global — the cleanup runs even on failure.
//
// Adding a new ux color or mode requires updating this one function;
// tests never need to enumerate the globals themselves.
func SaveState(t testing.TB) {
	t.Helper()
	origQuiet := ux.QuietMode
	origReset := ux.Reset
	origBold := ux.Bold
	origDim := ux.Dim
	origRed := ux.Red
	origGreen := ux.Green
	origYellow := ux.Yellow
	origCyan := ux.Cyan
	origMagenta := ux.Magenta
	origBlue := ux.Blue
	origBoldCyan := ux.BoldCyan
	origBoldBlue := ux.BoldBlue
	origBoldGreen := ux.BoldGreen
	origIsTerminal := ux.IsTerminal
	t.Cleanup(func() {
		ux.QuietMode = origQuiet
		ux.Reset = origReset
		ux.Bold = origBold
		ux.Dim = origDim
		ux.Red = origRed
		ux.Green = origGreen
		ux.Yellow = origYellow
		ux.Cyan = origCyan
		ux.Magenta = origMagenta
		ux.Blue = origBlue
		ux.BoldCyan = origBoldCyan
		ux.BoldBlue = origBoldBlue
		ux.BoldGreen = origBoldGreen
		ux.IsTerminal = origIsTerminal
	})
}
