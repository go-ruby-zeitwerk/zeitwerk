// Copyright (c) the go-ruby-zeitwerk/zeitwerk authors
//
// SPDX-License-Identifier: BSD-3-Clause

package zeitwerk

import "testing"

func TestCamelizeDefault(t *testing.T) {
	in := NewInflector()
	cases := map[string]string{
		"post":             "Post",
		"users_controller": "UsersController",
		"html_parser":      "HtmlParser", // default upcases only the first letter
		"api":              "Api",
		"v2":               "V2",
	}
	for basename, want := range cases {
		if got := in.Camelize(basename); got != want {
			t.Errorf("Camelize(%q) = %q, want %q", basename, got, want)
		}
	}
}

func TestCamelizeWholeBasenameOverride(t *testing.T) {
	in := NewInflector()
	in.Inflect(map[string]string{"html_parser": "HTMLParser", "mysql_adapter": "MySQLAdapter"})
	if got := in.Camelize("html_parser"); got != "HTMLParser" {
		t.Errorf("whole-basename override: got %q", got)
	}
	if got := in.Camelize("mysql_adapter"); got != "MySQLAdapter" {
		t.Errorf("whole-basename override: got %q", got)
	}
	// A basename with no override still uses the default rules.
	if got := in.Camelize("users_controller"); got != "UsersController" {
		t.Errorf("default alongside overrides: got %q", got)
	}
}

func TestCamelizePerWordOverride(t *testing.T) {
	in := NewInflector()
	in.Inflect(map[string]string{"html": "HTML"})
	if got := in.Camelize("html_parser"); got != "HTMLParser" {
		t.Errorf("per-word override: got %q, want HTMLParser", got)
	}
}

func TestCamelizeEmptyWords(t *testing.T) {
	in := NewInflector()
	// Double underscore yields an empty internal word (skipped), a trailing
	// separator drops its empty field, and a leading separator keeps it.
	if got := in.Camelize("foo__bar"); got != "FooBar" {
		t.Errorf("double underscore: got %q, want FooBar", got)
	}
	if got := in.Camelize("foo_"); got != "Foo" {
		t.Errorf("trailing underscore: got %q, want Foo", got)
	}
	if got := in.Camelize("_foo"); got != "Foo" {
		t.Errorf("leading underscore: got %q, want Foo", got)
	}
	if got := in.Camelize(""); got != "" {
		t.Errorf("empty basename: got %q, want empty", got)
	}
}
