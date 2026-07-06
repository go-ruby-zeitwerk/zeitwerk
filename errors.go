// Copyright (c) the go-ruby-zeitwerk/zeitwerk authors
//
// SPDX-License-Identifier: BSD-3-Clause

package zeitwerk

// Error is the base error type of the loader, mirroring Zeitwerk::Error (a
// StandardError subclass in the gem). PushDir on a directory that does not
// exist, and Reload without reloading enabled, both surface as an *Error, just
// as the gem raises Zeitwerk::Error for those conditions.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// NameError mirrors Zeitwerk::NameError (a ::NameError subclass in the gem). It
// models the failure raised when a managed file or directory does not define
// the constant its path maps to; the host binding raises it from its Load /
// DefineAutoload seam and it is surfaced here so callers can match on the type.
type NameError struct{ Msg string }

func (e *NameError) Error() string { return e.Msg }

// SetupRequired mirrors Zeitwerk::SetupRequired. EagerLoad and Reload return it
// when the loader has not been set up yet, exactly as the gem raises
// Zeitwerk::SetupRequired from eager_load and friends before setup has run.
type SetupRequired struct{}

func (e *SetupRequired) Error() string {
	return "Zeitwerk::Loader#setup has not been called yet"
}
