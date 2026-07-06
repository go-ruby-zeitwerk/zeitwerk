// Copyright (c) the go-ruby-zeitwerk/zeitwerk authors
//
// SPDX-License-Identifier: BSD-3-Clause

package zeitwerk

import "unicode"

// Inflector models Zeitwerk::Inflector, the default inflector that maps a file
// or directory basename to the constant name it defines. It is a faithful port
// of the gem's default_inflector: a very basic snake_case -> CamelCase
// conversion, honouring hard-coded overrides configured with Inflect.
//
// The reference Ruby is:
//
//	def camelize(basename, _abspath)
//	  inflections[basename] || basename.split("_").each_with_object("".dup) do |word, camelized|
//	    camelized << (inflections[word] || word[0].upcase << word[1..-1])
//	  end
//	end
//
// So Camelize first consults a whole-basename override, and otherwise splits on
// "_" and, for each word, consults a per-word override or upcases just the
// first character while leaving the rest of the word untouched. This is why the
// default turns "html_parser" into "HtmlParser" (not "HTMLParser") until an
// acronym override is registered.
type Inflector struct {
	overrides map[string]string
}

// NewInflector returns a default Zeitwerk inflector with no overrides.
func NewInflector() *Inflector {
	return &Inflector{overrides: map[string]string{}}
}

// Inflect registers hard-coded basename/word -> constant-name overrides,
// mirroring Zeitwerk::Inflector#inflect. Keys may be whole basenames
// ("html_parser" => "HTMLParser") or individual words ("html" => "HTML"); both
// are consulted by Camelize. Later calls merge into and override earlier ones.
func (in *Inflector) Inflect(m map[string]string) {
	for k, v := range m {
		in.overrides[k] = v
	}
}

// Camelize maps a snake_case basename to the constant name it defines, applying
// the same rules as Zeitwerk::Inflector#camelize. The abspath argument the gem
// accepts is not needed by the default inflector and is therefore omitted.
func (in *Inflector) Camelize(basename string) string {
	if over, ok := in.overrides[basename]; ok {
		return over
	}
	var b []rune
	for _, word := range splitUnderscore(basename) {
		if over, ok := in.overrides[word]; ok {
			b = append(b, []rune(over)...)
			continue
		}
		b = append(b, capitalizeFirst(word)...)
	}
	return string(b)
}

// capitalizeFirst upcases the first rune of word and leaves the remainder
// untouched, matching Ruby's `word[0].upcase << word[1..-1]`. An empty word
// contributes nothing (Ruby would raise on nil[0]; we degrade gracefully so a
// basename such as "foo__bar" or a trailing separator does not panic).
func capitalizeFirst(word string) []rune {
	rs := []rune(word)
	if len(rs) == 0 {
		return nil
	}
	rs[0] = unicode.ToUpper(rs[0])
	return rs
}

// splitUnderscore splits basename on "_" the way Ruby's String#split("_") does:
// internal and leading empty fields are kept, but trailing empty fields are
// dropped ("foo_".split("_") == ["foo"]).
func splitUnderscore(basename string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(basename); i++ {
		if basename[i] == '_' {
			parts = append(parts, basename[start:i])
			start = i + 1
		}
	}
	parts = append(parts, basename[start:])
	// Drop trailing empty fields, as Ruby's default split does.
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}
