// Copyright (c) the go-ruby-zeitwerk/zeitwerk authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package zeitwerk is a pure-Go (no cgo) model of the engine of Ruby's Zeitwerk
// autoloader, faithful to the observable behaviour of the zeitwerk gem's
// default configuration on MRI 4.0.5.
//
// It reimplements the part of Zeitwerk::Loader that is pure logic — scanning
// the managed directory trees and computing the bidirectional mapping between
// constant paths (e.g. "Admin::UsersController") and the file or directory that
// defines them, honouring namespaces, collapsed directories, and ignored paths,
// and driving the setup / eager-load / reload / unload lifecycle and its
// callbacks. Everything that actually touches a Ruby runtime — defining a
// constant, requiring a file, removing a constant — is left to the host through
// small function seams (DefineAutoload, Load, and the on_unload callback), so
// this package has no dependency on any Ruby runtime and is the reusable engine
// a go-embedded-ruby binding plugs into.
package zeitwerk

import (
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// DefineAutoloadFunc is the seam the host uses to register an autoload, standing
// in for Ruby's `Module#autoload`. Setup calls it once per managed constant
// with the fully-qualified constant path and the file or directory that defines
// it (isDir true for an implicit namespace module autovivified from a
// directory). It is optional; a nil seam means "engine only", useful for tests
// that just assert the computed map.
type DefineAutoloadFunc func(cpath, filePath string, isDir bool)

// LoadFunc is the seam the host uses to actually load a managed file, standing
// in for Ruby's `require`. EagerLoad calls it once per managed file; returning a
// non-nil error aborts the eager load with that error (the host may return a
// *NameError when the file does not define its expected constant).
type LoadFunc func(filePath string) error

// Autoload is one entry of the computed constant map: the constant a managed
// path defines.
type Autoload struct {
	Cpath string // fully-qualified constant path, e.g. "Admin::UsersController"
	Cname string // final segment only, e.g. "UsersController"
	Path  string // absolute file or directory path that defines it
	IsDir bool   // true for an implicit namespace module (a managed directory)
}

type root struct {
	path      string
	namespace string // "" for the top-level (Object) namespace
}

type onLoadCB struct {
	cpath string // "" means ANY constant
	fn    func(cpath, filePath string)
}

// Loader models Zeitwerk::Loader: a registry of root directories and the
// constant map computed from them. The zero value is not usable; construct one
// with NewLoader.
type Loader struct {
	inflector *Inflector
	fs        FS

	define DefineAutoloadFunc
	load   LoadFunc

	roots     []root
	ignores   []string
	collapses []string

	reloadable bool

	onLoads   []onLoadCB
	onSetups  []func()
	onUnloads []func(cpath, filePath string)

	autoloads map[string]Autoload
	setupDone bool
}

// NewLoader returns a loader with the default Zeitwerk inflector and the real
// filesystem. Configure it with PushDir / Ignore / Collapse and the seams, then
// call Setup.
func NewLoader() *Loader {
	return &Loader{
		inflector: NewInflector(),
		fs:        osFS{},
		autoloads: map[string]Autoload{},
	}
}

// Inflector returns the loader's inflector so callers can register overrides,
// e.g. Loader.Inflector().Inflect(map[string]string{"html_parser": "HTMLParser"}).
func (l *Loader) Inflector() *Inflector { return l.inflector }

// SetInflector replaces the inflector, mirroring `Zeitwerk::Loader#inflector=`.
func (l *Loader) SetInflector(in *Inflector) { l.inflector = in }

// SetFS replaces the filesystem seam the loader scans (default: the real
// filesystem).
func (l *Loader) SetFS(fs FS) { l.fs = fs }

// SetDefineAutoload installs the DefineAutoload seam (see DefineAutoloadFunc).
func (l *Loader) SetDefineAutoload(fn DefineAutoloadFunc) { l.define = fn }

// SetLoad installs the Load seam (see LoadFunc).
func (l *Loader) SetLoad(fn LoadFunc) { l.load = fn }

// EnableReloading permits Reload, mirroring `Zeitwerk::Loader#enable_reloading`.
// It must be called before Setup; Reload on a loader without it returns an
// *Error, as the gem raises Zeitwerk::Error.
func (l *Loader) EnableReloading() { l.reloadable = true }

// PushDir registers a root directory whose children map into namespace,
// mirroring `Zeitwerk::Loader#push_dir(path, namespace:)`. An empty namespace
// (or "Object") means the top level. The directory must exist; otherwise
// PushDir returns an *Error, as the gem raises Zeitwerk::Error for a missing
// root directory.
func (l *Loader) PushDir(dir, namespace string) error {
	// Normalize to forward slashes so all internal path logic (segment joining
	// via path.Join, glob matching via path.Match, path<->constant comparison)
	// is separator-agnostic. On Windows the caller may pass native backslash
	// paths (e.g. from t.TempDir() or filepath.Join); os.ReadDir/os.Stat accept
	// forward slashes there, so normalizing here is safe and makes the scan
	// behave identically on every OS.
	dir = filepath.ToSlash(dir)
	if _, err := l.fs.ReadDir(dir); err != nil {
		return &Error{Msg: "the root directory " + dir + " does not exist"}
	}
	if namespace == "Object" {
		namespace = ""
	}
	l.roots = append(l.roots, root{path: dir, namespace: namespace})
	return nil
}

// Ignore registers glob patterns whose matching files and directories are
// excluded from the managed tree, mirroring `Zeitwerk::Loader#ignore`. Patterns
// are matched against absolute paths with path.Match semantics (a pattern with
// no metacharacters matches that exact path).
func (l *Loader) Ignore(globs ...string) { l.ignores = append(l.ignores, toSlashAll(globs)...) }

// Collapse registers glob patterns of directories that do not represent a
// namespace: a collapsed directory's children are promoted into its parent
// namespace, mirroring `Zeitwerk::Loader#collapse`. Matching uses the same
// path.Match semantics as Ignore.
func (l *Loader) Collapse(globs ...string) { l.collapses = append(l.collapses, toSlashAll(globs)...) }

// OnLoad registers a callback fired when a managed constant is loaded (during
// EagerLoad here, since this engine has no lazy autoload trigger of its own),
// mirroring `Zeitwerk::Loader#on_load`. An empty cpath (or "ANY") fires for
// every constant; a specific cpath fires only for that one. This matches the
// gem, where `on_load(:ANY)` runs for all and `on_load("Foo")` for just Foo.
func (l *Loader) OnLoad(cpath string, fn func(cpath, filePath string)) {
	if cpath == "ANY" {
		cpath = ""
	}
	l.onLoads = append(l.onLoads, onLoadCB{cpath: cpath, fn: fn})
}

// OnSetup registers a callback fired at the end of every Setup (and thus after
// each Reload), mirroring `Zeitwerk::Loader#on_setup`.
func (l *Loader) OnSetup(fn func()) { l.onSetups = append(l.onSetups, fn) }

// OnUnload registers a callback fired for each managed constant when it is
// unloaded, mirroring `Zeitwerk::Loader#on_unload`. It is the host's seam for
// removing the constant, since removal touches the Ruby runtime.
func (l *Loader) OnUnload(fn func(cpath, filePath string)) {
	l.onUnloads = append(l.onUnloads, fn)
}

// Setup scans the registered root directories and builds the constant map,
// registering each managed constant through the DefineAutoload seam, mirroring
// `Zeitwerk::Loader#setup`. It then fires the on_setup callbacks. Setup is
// idempotent: calling it again while already set up is a no-op.
func (l *Loader) Setup() error {
	if l.setupDone {
		return nil
	}
	l.autoloads = map[string]Autoload{}
	for _, r := range l.roots {
		if err := l.scan(r.path, r.namespace); err != nil {
			return err
		}
	}
	for _, a := range l.sortedAutoloads() {
		if l.define != nil {
			l.define(a.Cpath, a.Path, a.IsDir)
		}
	}
	l.setupDone = true
	for _, fn := range l.onSetups {
		fn()
	}
	return nil
}

// scan recursively walks dir, recording the constants its entries define under
// parent (the current namespace cpath, "" at the top level).
func (l *Loader) scan(dir, parent string) error {
	entries, err := l.fs.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range sortedEntries(entries) {
		if strings.HasPrefix(e.Name, ".") {
			continue // hidden files and directories are never managed
		}
		abspath := path.Join(dir, e.Name)
		if l.ignored(abspath) {
			continue
		}
		if e.IsDir {
			if l.collapsed(abspath) {
				// A collapsed directory is not a namespace: scan its children
				// straight into the current namespace.
				if err := l.scan(abspath, parent); err != nil {
					return err
				}
				continue
			}
			cname := l.inflector.Camelize(e.Name)
			cpath := joinCpath(parent, cname)
			l.record(Autoload{Cpath: cpath, Cname: cname, Path: abspath, IsDir: true})
			if err := l.scan(abspath, cpath); err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(e.Name, ".rb") {
			continue // only .rb files define constants
		}
		basename := strings.TrimSuffix(e.Name, ".rb")
		cname := l.inflector.Camelize(basename)
		cpath := joinCpath(parent, cname)
		l.record(Autoload{Cpath: cpath, Cname: cname, Path: abspath, IsDir: false})
	}
	return nil
}

// record stores a, resolving an explicit-namespace collision (a file and a
// directory mapping to the same constant) in favour of the file: the .rb file
// is the definer, while the directory still contributes its children.
func (l *Loader) record(a Autoload) {
	if prev, ok := l.autoloads[a.Cpath]; ok && !prev.IsDir {
		return // a file already defines this constant; keep it as the definer
	}
	l.autoloads[a.Cpath] = a
}

// EagerLoad loads every managed file and fires the on_load callbacks for every
// managed constant, mirroring `Zeitwerk::Loader#eager_load`. Constants are
// processed in sorted order so parent namespaces precede their children. It
// returns a *SetupRequired if Setup has not run, and propagates the first
// error the Load seam returns.
func (l *Loader) EagerLoad() error {
	if !l.setupDone {
		return &SetupRequired{}
	}
	for _, a := range l.sortedAutoloads() {
		if !a.IsDir && l.load != nil {
			if err := l.load(a.Path); err != nil {
				return err
			}
		}
		l.fireOnLoad(a)
	}
	return nil
}

// fireOnLoad runs the on_load callbacks matching a's constant path.
func (l *Loader) fireOnLoad(a Autoload) {
	for _, cb := range l.onLoads {
		if cb.cpath == "" || cb.cpath == a.Cpath {
			cb.fn(a.Cpath, a.Path)
		}
	}
}

// Unload fires the on_unload callback for every managed constant and clears the
// computed map, requiring a fresh Setup afterwards, mirroring
// `Zeitwerk::Loader#unload`.
func (l *Loader) Unload() {
	for _, a := range l.sortedAutoloads() {
		for _, fn := range l.onUnloads {
			fn(a.Cpath, a.Path)
		}
	}
	l.autoloads = map[string]Autoload{}
	l.setupDone = false
}

// Reload unloads and sets up again, picking up filesystem changes, mirroring
// `Zeitwerk::Loader#reload`. It returns an *Error if reloading was not enabled
// with EnableReloading (the gem raises Zeitwerk::Error), and a *SetupRequired
// if Setup has not run yet.
func (l *Loader) Reload() error {
	if !l.reloadable {
		return &Error{Msg: "can't reload, please call loader.enable_reloading before setup"}
	}
	if !l.setupDone {
		return &SetupRequired{}
	}
	l.Unload()
	return l.Setup()
}

// Autoloads returns the computed constant map as a slice sorted by constant
// path, a stable snapshot suitable for assertions and for a host that wants to
// enumerate the managed constants.
func (l *Loader) Autoloads() []Autoload { return l.sortedAutoloads() }

// CpathAt returns the constant a given file or directory path defines, the
// inverse direction of the map (path -> constant). The boolean is false if the
// path is not managed.
func (l *Loader) CpathAt(filePath string) (Autoload, bool) {
	filePath = filepath.ToSlash(filePath)
	for _, a := range l.autoloads {
		if a.Path == filePath {
			return a, true
		}
	}
	return Autoload{}, false
}

// PathAt returns the managed path a given constant path maps to (constant ->
// path). The boolean is false if the constant is not managed.
func (l *Loader) PathAt(cpath string) (Autoload, bool) {
	a, ok := l.autoloads[cpath]
	return a, ok
}

func (l *Loader) sortedAutoloads() []Autoload {
	out := make([]Autoload, 0, len(l.autoloads))
	for _, a := range l.autoloads {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cpath < out[j].Cpath })
	return out
}

// ignored reports whether abspath matches any registered ignore glob.
func (l *Loader) ignored(abspath string) bool { return matchAny(l.ignores, abspath) }

// collapsed reports whether abspath matches any registered collapse glob.
func (l *Loader) collapsed(abspath string) bool { return matchAny(l.collapses, abspath) }

// matchAny reports whether abspath equals or path.Match-matches any pattern.
func matchAny(patterns []string, abspath string) bool {
	for _, p := range patterns {
		if p == abspath {
			return true
		}
		if ok, err := path.Match(p, abspath); err == nil && ok {
			return true
		}
	}
	return false
}

// toSlashAll returns globs with every element normalized to forward slashes, so
// ignore/collapse patterns given with native separators match the loader's
// forward-slash internal paths on every OS.
func toSlashAll(globs []string) []string {
	out := make([]string, len(globs))
	for i, g := range globs {
		out[i] = filepath.ToSlash(g)
	}
	return out
}

// joinCpath appends cname to the parent constant path, producing "Cname" at the
// top level and "Parent::Cname" within a namespace.
func joinCpath(parent, cname string) string {
	if parent == "" {
		return cname
	}
	return parent + "::" + cname
}
