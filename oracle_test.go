// Copyright (c) the go-ruby-zeitwerk/zeitwerk authors
//
// SPDX-License-Identifier: BSD-3-Clause

package zeitwerk

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// rubyBin locates a ruby that has the zeitwerk gem, once. The oracle tests skip
// themselves when ruby or the gem is absent (the Windows lane and the qemu
// cross-arch lanes), so the deterministic, ruby-free suite alone drives the
// 100% coverage gate; the oracle is a faithfulness check that runs on developer
// machines and the ubuntu/macos lanes where the gem is installed.
func rubyBin(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip("ruby not on PATH; skipping MRI oracle")
	}
	if err := exec.Command(path, "-e", `require "zeitwerk"`).Run(); err != nil {
		t.Skip("zeitwerk gem not installed; skipping MRI oracle")
	}
	return path
}

// mri runs a Ruby snippet (with optional args) and returns its trimmed stdout.
func mri(t *testing.T, ruby, src string, args ...string) string {
	t.Helper()
	out, err := exec.Command(ruby, append([]string{"-e", src}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("ruby failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestOracleInflectorDefault checks the default snake_case -> CamelCase mapping
// against the gem's Zeitwerk::Inflector for a spread of basenames.
func TestOracleInflectorDefault(t *testing.T) {
	ruby := rubyBin(t)
	names := []string{"post", "users_controller", "html_parser", "api", "v2", "foo_bar_baz"}
	want := mri(t, ruby, `
require "zeitwerk"
inf = Zeitwerk::Inflector.new
puts ARGV.map { |n| inf.camelize(n, "") }.join(",")`, names...)

	in := NewInflector()
	var got []string
	for _, n := range names {
		got = append(got, in.Camelize(n))
	}
	if strings.Join(got, ",") != want {
		t.Fatalf("default inflector: go=%q mri=%q", strings.Join(got, ","), want)
	}
}

// TestOracleInflectorAcronym checks that a whole-basename inflect override
// matches the gem for acronym constants.
func TestOracleInflectorAcronym(t *testing.T) {
	ruby := rubyBin(t)
	want := mri(t, ruby, `
require "zeitwerk"
inf = Zeitwerk::Inflector.new
inf.inflect("html_parser" => "HTMLParser", "mysql_adapter" => "MySQLAdapter")
puts [inf.camelize("html_parser", ""), inf.camelize("mysql_adapter", ""), inf.camelize("users_controller", "")].join(",")`)

	in := NewInflector()
	in.Inflect(map[string]string{"html_parser": "HTMLParser", "mysql_adapter": "MySQLAdapter"})
	got := strings.Join([]string{in.Camelize("html_parser"), in.Camelize("mysql_adapter"), in.Camelize("users_controller")}, ",")
	if got != want {
		t.Fatalf("acronym inflector: go=%q mri=%q", got, want)
	}
}

// TestOracleSetupMap builds a real directory tree and checks that our computed
// constant map matches Zeitwerk::Loader#all_expected_cpaths on the very same
// tree — including a nested implicit namespace, a collapsed directory, and an
// ignored file.
func TestOracleSetupMap(t *testing.T) {
	ruby := rubyBin(t)
	root := t.TempDir()
	for _, rel := range []string{
		"users_controller.rb",
		"html_parser.rb",
		"admin/users_controller.rb",
		"models/concerns/auditable.rb",
		"ignored.rb",
		"notes.txt",
	} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The gem's own view of the constant map on this tree. A collapsed directory
	// is itself reported by all_expected_cpaths as mapping to the namespace it
	// collapses into, so we compare the unique set of managed constants.
	want := mri(t, ruby, `
require "zeitwerk"
root = ARGV[0]
loader = Zeitwerk::Loader.new
loader.push_dir(root)
loader.collapse(File.join(root, "models", "concerns"))
loader.ignore(File.join(root, "ignored.rb"))
loader.setup
cpaths = loader.all_expected_cpaths.values.reject { |c| c == "Object" }
puts cpaths.uniq.sort.join("\n")`, root)

	l := NewLoader()
	if err := l.PushDir(root, "Object"); err != nil {
		t.Fatal(err)
	}
	l.Collapse(filepath.Join(root, "models", "concerns"))
	l.Ignore(filepath.Join(root, "ignored.rb"))
	if err := l.Setup(); err != nil {
		t.Fatal(err)
	}
	got := cpaths(l)
	sort.Strings(got)
	if strings.Join(got, "\n") != want {
		t.Fatalf("setup map mismatch:\n go=%q\nmri=%q", strings.Join(got, "\n"), want)
	}
}
