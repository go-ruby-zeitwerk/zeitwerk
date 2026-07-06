// Copyright (c) the go-ruby-zeitwerk/zeitwerk authors
//
// SPDX-License-Identifier: BSD-3-Clause

package zeitwerk

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// memFS is an in-memory FS for deterministic scan tests: dirs maps a directory
// path to its children, and any path in fail returns an error from ReadDir.
type memFS struct {
	dirs map[string][]DirEntry
	fail map[string]bool
}

func (m memFS) ReadDir(dir string) ([]DirEntry, error) {
	if m.fail[dir] {
		return nil, fmt.Errorf("boom: %s", dir)
	}
	es, ok := m.dirs[dir]
	if !ok {
		return nil, fmt.Errorf("no such dir: %s", dir)
	}
	return es, nil
}

func f(name string) DirEntry { return DirEntry{Name: name} }
func d(name string) DirEntry { return DirEntry{Name: name, IsDir: true} }

// cpaths returns the sorted constant paths of a loader's map.
func cpaths(l *Loader) []string {
	var out []string
	for _, a := range l.Autoloads() {
		out = append(out, a.Cpath)
	}
	sort.Strings(out)
	return out
}

func TestSetupMapOnTempTree(t *testing.T) {
	// A real directory tree exercises the default osFS end to end.
	root := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("users_controller.rb")
	write("html_parser.rb")
	write("admin/users_controller.rb")
	write("models/concerns/auditable.rb")
	write("notes.txt")      // non-.rb, ignored
	write(".hidden/foo.rb") // hidden dir, skipped

	l := NewLoader()
	l.Inflector().Inflect(map[string]string{"html_parser": "HTMLParser"})
	if err := l.PushDir(root, "Object"); err != nil {
		t.Fatal(err)
	}
	l.Collapse(filepath.Join(root, "models", "concerns"))
	if err := l.Setup(); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"Admin", "Admin::UsersController",
		"HTMLParser",
		"Models", "Models::Auditable",
		"UsersController",
	}
	if got := cpaths(l); !reflect.DeepEqual(got, want) {
		t.Fatalf("cpaths = %v, want %v", got, want)
	}

	// Both directions of the map resolve.
	if a, ok := l.PathAt("Admin::UsersController"); !ok || a.Path != filepath.Join(root, "admin", "users_controller.rb") {
		t.Fatalf("PathAt(Admin::UsersController) = %+v ok=%v", a, ok)
	}
	if a, ok := l.CpathAt(filepath.Join(root, "admin")); !ok || a.Cpath != "Admin" || !a.IsDir {
		t.Fatalf("CpathAt(admin dir) = %+v ok=%v", a, ok)
	}
	if _, ok := l.PathAt("Nope"); ok {
		t.Fatal("PathAt(Nope) should be absent")
	}
	if _, ok := l.CpathAt("/nope"); ok {
		t.Fatal("CpathAt(/nope) should be absent")
	}
}

func TestPushDirMissing(t *testing.T) {
	l := NewLoader()
	err := l.PushDir(filepath.Join(t.TempDir(), "does-not-exist"), "")
	var ze *Error
	if err == nil || !asError(err, &ze) {
		t.Fatalf("PushDir on missing dir: err=%v", err)
	}
}

func TestNamespaceAndIgnore(t *testing.T) {
	fs := memFS{dirs: map[string][]DirEntry{
		"app":       {f("widget.rb"), f("skip_me.rb")},
		"app/admin": nil,
	}}
	// A namespaced root plus an ignored file.
	l := NewLoader()
	l.SetFS(fs)
	fs.dirs["app"] = append(fs.dirs["app"], d("admin"))
	if err := l.PushDir("app", "Api"); err != nil {
		t.Fatal(err)
	}
	l.Ignore("app/skip_me.rb")
	if err := l.Setup(); err != nil {
		t.Fatal(err)
	}
	want := []string{"Api::Admin", "Api::Widget"}
	if got := cpaths(l); !reflect.DeepEqual(got, want) {
		t.Fatalf("cpaths = %v, want %v", got, want)
	}
}

func TestScanReadDirError(t *testing.T) {
	fs := memFS{
		dirs: map[string][]DirEntry{"root": {d("sub")}},
		fail: map[string]bool{"root/sub": true},
	}
	l := NewLoader()
	l.SetFS(fs)
	if err := l.PushDir("root", ""); err != nil {
		t.Fatal(err)
	}
	if err := l.Setup(); err == nil {
		t.Fatal("Setup should propagate the ReadDir error from a subdirectory")
	}
}

func TestExplicitNamespaceFileWins(t *testing.T) {
	// A directory "admin" and a file "admin.rb" both map to "Admin"; the file is
	// the definer while the directory contributes its children. "admin" sorts
	// before "admin.rb", so the directory is recorded first and then overwritten.
	fs := memFS{dirs: map[string][]DirEntry{
		"root":       {d("admin"), f("admin.rb")},
		"root/admin": {f("dashboard.rb")},
	}}
	l := NewLoader()
	l.SetFS(fs)
	if err := l.PushDir("root", ""); err != nil {
		t.Fatal(err)
	}
	if err := l.Setup(); err != nil {
		t.Fatal(err)
	}
	a, _ := l.PathAt("Admin")
	if a.IsDir {
		t.Fatalf("Admin should be defined by the file, got dir entry %+v", a)
	}
	if a.Path != "root/admin.rb" {
		t.Fatalf("Admin path = %q, want root/admin.rb", a.Path)
	}
	if _, ok := l.PathAt("Admin::Dashboard"); !ok {
		t.Fatal("directory children should still be scanned under Admin")
	}
}

func TestFileDefinerNotClobberedByLaterDir(t *testing.T) {
	// Two entries collide on constant "X": file "a.rb" (sorts first, records the
	// file) then dir "b" (recorded second, must not clobber the file definer).
	fs := memFS{dirs: map[string][]DirEntry{
		"root":   {f("a.rb"), d("b")},
		"root/b": nil,
	}}
	l := NewLoader()
	l.SetFS(fs)
	l.Inflector().Inflect(map[string]string{"a": "X", "b": "X"})
	if err := l.PushDir("root", ""); err != nil {
		t.Fatal(err)
	}
	if err := l.Setup(); err != nil {
		t.Fatal(err)
	}
	a, _ := l.PathAt("X")
	if a.IsDir || a.Path != "root/a.rb" {
		t.Fatalf("file must win the collision, got %+v", a)
	}
}

func TestSetupIdempotentAndCallbacks(t *testing.T) {
	fs := memFS{dirs: map[string][]DirEntry{"root": {f("post.rb")}}}
	l := NewLoader()
	l.SetFS(fs)

	var defined [][3]any
	l.SetDefineAutoload(func(cpath, p string, isDir bool) {
		defined = append(defined, [3]any{cpath, p, isDir})
	})
	setups := 0
	l.OnSetup(func() { setups++ })

	if err := l.PushDir("root", ""); err != nil {
		t.Fatal(err)
	}
	if err := l.Setup(); err != nil {
		t.Fatal(err)
	}
	if err := l.Setup(); err != nil { // idempotent: no-op while already set up
		t.Fatal(err)
	}
	if len(defined) != 1 || defined[0][0] != "Post" {
		t.Fatalf("DefineAutoload calls = %v", defined)
	}
	if setups != 1 {
		t.Fatalf("on_setup fired %d times, want 1", setups)
	}
}

func TestEagerLoadAndOnLoad(t *testing.T) {
	fs := memFS{dirs: map[string][]DirEntry{
		"root":       {f("post.rb"), d("admin")},
		"root/admin": {f("user.rb")},
	}}
	l := NewLoader()
	l.SetFS(fs)

	var loaded []string
	l.SetLoad(func(p string) error { loaded = append(loaded, p); return nil })
	var anyC, postC []string
	l.OnLoad("ANY", func(cpath, _ string) { anyC = append(anyC, cpath) })
	l.OnLoad("Post", func(cpath, _ string) { postC = append(postC, cpath) })

	if err := l.EagerLoad(); err == nil {
		t.Fatal("EagerLoad before Setup must return SetupRequired")
	} else if _, ok := err.(*SetupRequired); !ok {
		t.Fatalf("want *SetupRequired, got %T", err)
	}

	if err := l.PushDir("root", ""); err != nil {
		t.Fatal(err)
	}
	if err := l.Setup(); err != nil {
		t.Fatal(err)
	}
	if err := l.EagerLoad(); err != nil {
		t.Fatal(err)
	}
	// Only the two .rb files are loaded; the directory namespace is not.
	wantLoaded := []string{"root/admin/user.rb", "root/post.rb"}
	if !reflect.DeepEqual(loaded, wantLoaded) {
		t.Fatalf("loaded = %v, want %v", loaded, wantLoaded)
	}
	wantAny := []string{"Admin", "Admin::User", "Post"}
	if !reflect.DeepEqual(anyC, wantAny) {
		t.Fatalf("ANY on_load = %v, want %v", anyC, wantAny)
	}
	if !reflect.DeepEqual(postC, []string{"Post"}) {
		t.Fatalf("Post on_load = %v", postC)
	}
}

func TestEagerLoadError(t *testing.T) {
	fs := memFS{dirs: map[string][]DirEntry{"root": {f("post.rb")}}}
	l := NewLoader()
	l.SetFS(fs)
	boom := &NameError{Msg: "expected file root/post.rb to define constant Post"}
	l.SetLoad(func(string) error { return boom })
	if err := l.PushDir("root", ""); err != nil {
		t.Fatal(err)
	}
	if err := l.Setup(); err != nil {
		t.Fatal(err)
	}
	if err := l.EagerLoad(); err != boom {
		t.Fatalf("EagerLoad error = %v, want %v", err, boom)
	}
}

func TestReloadAndUnload(t *testing.T) {
	fs := memFS{dirs: map[string][]DirEntry{"root": {f("post.rb")}}}
	l := NewLoader()
	l.SetFS(fs)

	var unloaded []string
	l.OnUnload(func(cpath, _ string) { unloaded = append(unloaded, cpath) })

	// Reload before enabling reloading is an *Error.
	if err := l.Reload(); err == nil {
		t.Fatal("Reload without EnableReloading must error")
	} else if _, ok := err.(*Error); !ok {
		t.Fatalf("want *Error, got %T", err)
	}

	l.EnableReloading()
	if err := l.PushDir("root", ""); err != nil {
		t.Fatal(err)
	}

	// Reload before Setup is a *SetupRequired.
	if err := l.Reload(); err == nil {
		t.Fatal("Reload before Setup must return SetupRequired")
	} else if _, ok := err.(*SetupRequired); !ok {
		t.Fatalf("want *SetupRequired, got %T", err)
	}

	if err := l.Setup(); err != nil {
		t.Fatal(err)
	}
	// A new file appears; reload picks it up and unloads the old constants.
	fs.dirs["root"] = []DirEntry{f("post.rb"), f("comment.rb")}
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(unloaded, []string{"Post"}) {
		t.Fatalf("on_unload = %v, want [Post]", unloaded)
	}
	if got := cpaths(l); !reflect.DeepEqual(got, []string{"Comment", "Post"}) {
		t.Fatalf("after reload cpaths = %v", got)
	}

	// Explicit Unload clears the map and requires setup again.
	l.Unload()
	if len(l.Autoloads()) != 0 {
		t.Fatal("Unload should clear the map")
	}
	if _, ok := l.PathAt("Post"); ok {
		t.Fatal("Unload should drop constants")
	}
}

func TestCollapseScanErrorAndGlob(t *testing.T) {
	// A glob collapse pattern matches the concerns dir; the recursive scan into
	// that collapsed dir then fails, and the error propagates out of Setup.
	fs := memFS{
		dirs: map[string][]DirEntry{"root": {d("concerns")}},
		fail: map[string]bool{"root/concerns": true},
	}
	l := NewLoader()
	l.SetFS(fs)
	l.Collapse("root/*") // glob (path.Match) rather than an exact path
	if err := l.PushDir("root", ""); err != nil {
		t.Fatal(err)
	}
	if err := l.Setup(); err == nil {
		t.Fatal("Setup should propagate the error from scanning a collapsed dir")
	}
}

func TestIgnoreGlob(t *testing.T) {
	// A glob ignore pattern (path.Match) excludes every .rb under root here.
	fs := memFS{dirs: map[string][]DirEntry{"root": {f("a.rb"), f("b.rb")}}}
	l := NewLoader()
	l.SetFS(fs)
	l.Ignore("root/*.rb")
	if err := l.PushDir("root", ""); err != nil {
		t.Fatal(err)
	}
	if err := l.Setup(); err != nil {
		t.Fatal(err)
	}
	if got := l.Autoloads(); len(got) != 0 {
		t.Fatalf("glob ignore left %v", got)
	}
}

func TestSetInflectorAndMatchErrors(t *testing.T) {
	fs := memFS{dirs: map[string][]DirEntry{"root": {f("thing.rb")}}}
	l := NewLoader()
	l.SetFS(fs)
	l.SetInflector(NewInflector())
	// A malformed glob pattern must be treated as no-match, not a panic.
	l.Ignore("[")
	l.Collapse("[")
	if err := l.PushDir("root", ""); err != nil {
		t.Fatal(err)
	}
	if err := l.Setup(); err != nil {
		t.Fatal(err)
	}
	if got := cpaths(l); !reflect.DeepEqual(got, []string{"Thing"}) {
		t.Fatalf("cpaths = %v, want [Thing]", got)
	}
}

func TestPushDirNativeSeparatorPath(t *testing.T) {
	// Windows regression: a root (and ignore/collapse globs) given with the OS's
	// native separator must yield the same constant map as the forward-slash
	// form. On Windows filepath.FromSlash produces backslash paths — exactly what
	// t.TempDir()/filepath.Join hand the binding — which previously mis-parsed
	// because the loader does /-based path logic internally. filepath.ToSlash in
	// PushDir/Ignore/Collapse/CpathAt normalizes them. On Unix FromSlash is the
	// identity, so this still passes there while the windows-latest CI lane
	// actually exercises the backslash path.
	build := func(root string) *Loader {
		fs := memFS{dirs: map[string][]DirEntry{
			"app/pkg":       {d("admin"), f("user.rb"), f("skip.rb")},
			"app/pkg/admin": {f("dashboard.rb")},
		}}
		l := NewLoader()
		l.SetFS(fs)
		if err := l.PushDir(root, ""); err != nil {
			t.Fatal(err)
		}
		l.Ignore(filepath.FromSlash("app/pkg/skip.rb"))
		if err := l.Setup(); err != nil {
			t.Fatal(err)
		}
		return l
	}

	slash := cpaths(build("app/pkg"))
	native := cpaths(build(filepath.FromSlash("app/pkg")))
	if !reflect.DeepEqual(native, slash) {
		t.Fatalf("native-separator root gave %v, want %v (same as /-form)", native, slash)
	}
	want := []string{"Admin", "Admin::Dashboard", "User"}
	if !reflect.DeepEqual(slash, want) {
		t.Fatalf("cpaths = %v, want %v", slash, want)
	}

	// The path->constant lookup must also resolve a native-separator path.
	l := build(filepath.FromSlash("app/pkg"))
	if a, ok := l.CpathAt(filepath.FromSlash("app/pkg/admin")); !ok || a.Cpath != "Admin" {
		t.Fatalf("CpathAt(native admin dir) = %+v ok=%v", a, ok)
	}
}

// asError reports whether err is a *Error, storing it into target.
func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}
