// Copyright (c) the go-ruby-zeitwerk/zeitwerk authors
//
// SPDX-License-Identifier: BSD-3-Clause

package zeitwerk

import "testing"

func TestErrorMessages(t *testing.T) {
	if got := (&Error{Msg: "boom"}).Error(); got != "boom" {
		t.Errorf("Error.Error() = %q", got)
	}
	if got := (&NameError{Msg: "no constant"}).Error(); got != "no constant" {
		t.Errorf("NameError.Error() = %q", got)
	}
	if got := (&SetupRequired{}).Error(); got != "Zeitwerk::Loader#setup has not been called yet" {
		t.Errorf("SetupRequired.Error() = %q", got)
	}
}
