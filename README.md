<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-zeitwerk/brand/main/social/go-ruby-zeitwerk-zeitwerk.png" alt="go-ruby-zeitwerk/zeitwerk" width="720"></p>

# zeitwerk — go-ruby-zeitwerk

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-zeitwerk.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) model of the engine of Ruby's [`Zeitwerk`](https://github.com/fxn/zeitwerk) autoloader**
— the code loader behind Rails and countless gems — faithful to the observable
behaviour of the `zeitwerk` gem's default configuration on MRI 4.0.5. It mirrors
Zeitwerk's snake_case→CamelCase inflection (with acronym overrides), its
`push_dir` / `ignore` / `collapse` / namespace configuration, and the way `setup`
turns a directory tree into the bidirectional map between **constant paths and
the files or directories that define them** — the map that then drives
`eager_load`, `reload`, `unload`, and the `on_load` / `on_setup` / `on_unload`
callbacks — **without any Ruby runtime**.

It is the `Zeitwerk` backend for
[go-embedded-ruby](https://github.com/go-embedded-ruby/ruby), but is a
**standalone, reusable** module with no dependency on the Ruby runtime — a sibling
of [go-ruby-set](https://github.com/go-ruby-set/set) and
[go-ruby-connection-pool](https://github.com/go-ruby-connection-pool/connection-pool).

> **MRI-faithful, not Composition-Oriented.** This is the *gem*'s loader — every
> mapping decision matches what MRI 4.0.5 + `zeitwerk` does, verified by a
> differential oracle that runs the real gem's `Zeitwerk::Inflector` and
> `Zeitwerk::Loader#all_expected_cpaths` side by side with this package.

## Engine, not runtime — the two seams

Zeitwerk does two very different kinds of work. Most of it is **pure logic**:
scanning the managed directories and computing which constant each `.rb` file and
each subdirectory should define, honouring the configured namespaces, collapsed
directories, and ignored paths. That logic is what this package reimplements, and
it needs no Ruby.

The rest **touches the Ruby runtime** — registering a `Module#autoload`, running
`require`, removing a constant on unload. Those are left to the host through two
function seams, so `require "zeitwerk"` in [go-embedded-ruby](https://github.com/go-embedded-ruby/ruby)
plugs the real VM in here while everything else stays pure Go:

```go
// DefineAutoload registers an autoload (Ruby's Module#autoload); Setup calls it
// once per managed constant. isDir marks an implicit namespace module.
type DefineAutoloadFunc func(cpath, filePath string, isDir bool)

// Load actually loads a managed file (Ruby's require); EagerLoad calls it once
// per managed file and aborts on the first error it returns.
type LoadFunc func(filePath string) error
```

Constant *removal* on unload is the third runtime touch-point; it rides the
`on_unload` callback, which the host uses to drop the constant.

## The filesystem is a seam too

The directory scan reads through an injectable `FS` (default: the real
filesystem), so a host that manages a virtual tree — or a test that builds one in
a temp dir — plugs its own in:

```go
type FS interface {
	ReadDir(dir string) ([]DirEntry, error)
}
```

## Inflection

`Inflector` is a faithful port of `Zeitwerk::Inflector`, the default inflector.
It is deliberately *basic*: it upcases only the first letter of each underscored
word, so `html_parser` becomes `HtmlParser` **until** you register an acronym.

```go
in := zeitwerk.NewInflector()
in.Camelize("users_controller")           // => "UsersController"
in.Camelize("html_parser")                // => "HtmlParser"  (default)

in.Inflect(map[string]string{"html_parser": "HTMLParser"})
in.Camelize("html_parser")                // => "HTMLParser"  (override)
```

## Usage

```go
loader := zeitwerk.NewLoader()
loader.Inflector().Inflect(map[string]string{"html_parser": "HTMLParser"})

loader.PushDir("/app/models", "")          // top-level namespace
loader.Ignore("/app/models/legacy.rb")
loader.Collapse("/app/models/concerns")    // not a namespace; promote children

loader.SetDefineAutoload(func(cpath, path string, isDir bool) { /* Module#autoload */ })
loader.SetLoad(func(path string) error { /* require */ return nil })
loader.OnLoad("ANY", func(cpath, path string) { /* after each constant loads */ })

loader.Setup()        // scan → build the constant map → register autoloads
loader.EagerLoad()    // load every managed file, firing on_load
// ... later, after files change:
loader.EnableReloading()   // (before Setup, in practice)
loader.Reload()            // unload + setup again
```

`Autoloads()` returns the computed map (sorted by constant path); `PathAt` and
`CpathAt` resolve it in either direction.

## API

```go
// Inflector — models Zeitwerk::Inflector (the default_inflector)
func NewInflector() *Inflector
func (in *Inflector) Inflect(m map[string]string)          // inflect
func (in *Inflector) Camelize(basename string) string      // camelize

// Loader — models Zeitwerk::Loader
func NewLoader() *Loader
func (l *Loader) Inflector() *Inflector                     // inflector
func (l *Loader) SetInflector(in *Inflector)                // inflector=
func (l *Loader) SetFS(fs FS)                               // (filesystem seam)
func (l *Loader) SetDefineAutoload(fn DefineAutoloadFunc)   // (Module#autoload seam)
func (l *Loader) SetLoad(fn LoadFunc)                       // (require seam)
func (l *Loader) EnableReloading()                          // enable_reloading
func (l *Loader) PushDir(dir, namespace string) error       // push_dir(path, namespace:)
func (l *Loader) Ignore(globs ...string)                    // ignore
func (l *Loader) Collapse(globs ...string)                  // collapse
func (l *Loader) OnLoad(cpath string, fn func(cpath, filePath string)) // on_load(:ANY | "Cpath")
func (l *Loader) OnSetup(fn func())                         // on_setup
func (l *Loader) OnUnload(fn func(cpath, filePath string))  // on_unload
func (l *Loader) Setup() error                              // setup
func (l *Loader) EagerLoad() error                          // eager_load
func (l *Loader) Reload() error                             // reload
func (l *Loader) Unload()                                   // unload
func (l *Loader) Autoloads() []Autoload                     // the computed map
func (l *Loader) PathAt(cpath string) (Autoload, bool)      // constant -> path
func (l *Loader) CpathAt(filePath string) (Autoload, bool)  // path -> constant

// Errors — model the gem's hierarchy
type Error struct{ Msg string }          // Zeitwerk::Error
type NameError struct{ Msg string }      // Zeitwerk::NameError
type SetupRequired struct{}              // Zeitwerk::SetupRequired
```

## Semantics

- **The map.** `Setup` walks each root and records, for every non-hidden `.rb`
  file and every subdirectory, the fully-qualified constant it defines. A
  subdirectory is an **implicit namespace** module (`admin/` → `Admin`); a file
  and a same-named directory (`admin.rb` + `admin/`) form an **explicit
  namespace** where the file is the definer and the directory contributes its
  children. Hidden entries (dotfiles) and non-`.rb` files are skipped.
- **Namespaces.** `PushDir(dir, "Admin")` maps the root's children under `Admin`;
  an empty namespace (or `"Object"`) means the top level.
- **Ignore / collapse.** `Ignore` drops matching paths (and their descendants);
  `Collapse` marks a directory as *not* a namespace, promoting its children into
  the parent. Both take glob patterns matched against absolute paths with
  `path.Match` semantics.
- **Lifecycle.** `EagerLoad` loads every managed file and fires `on_load`;
  `Unload` fires `on_unload` and clears the map; `Reload` (only after
  `EnableReloading`) is `Unload` + `Setup`. `EagerLoad` / `Reload` return a
  `*SetupRequired` before `Setup` has run.

## Fidelity vs the `zeitwerk` gem

The default inflector is ported exactly, including its one quirk: the reference

```ruby
inflections[basename] || basename.split("_").each_with_object("".dup) { |word, s|
  s << (inflections[word] || word[0].upcase << word[1..-1]) }
```

only upcases the **first** character of each word (so `html_parser` → `HtmlParser`,
not `HTMLParser`, until an override is registered) — this package does the same,
and the differential oracle asserts it against a real `Zeitwerk::Inflector`. The
constant map is checked against the gem's own `Zeitwerk::Loader#all_expected_cpaths`
on an identical directory tree covering nested namespaces, a collapsed directory,
and an ignored file. Glob matching uses Go's `path.Match` (a faithful subset of
`Dir.glob`; `**` is not expanded), and empty inflection words degrade gracefully
where MRI would raise on `nil[0]`.

## Tests & coverage

```sh
go test -race ./...
```

The suite holds **100% line coverage** — inflection (defaults, per-word and
whole-basename overrides, acronyms, empty-word edges), `push_dir` / `ignore` /
`collapse` / namespaces, setup-map correctness on real and injected trees,
explicit-namespace collisions, `eager_load`, the callbacks, and every error
branch — and cross-compiles on all six supported 64-bit targets (`amd64`,
`arm64`, `riscv64`, `loong64`, `ppc64le`, `s390x`, including the big-endian
`s390x`). A differential **MRI oracle** runs the real `zeitwerk` gem side by side
with this package on the lanes where Ruby and the gem are present; it skips
itself elsewhere, so the deterministic, Ruby-free tests alone drive the coverage
gate.

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-ruby-zeitwerk/zeitwerk authors.
