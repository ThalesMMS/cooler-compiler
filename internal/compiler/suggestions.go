// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

// Searches fail closed on exhaustion, avoiding map-order dependent partial
// suggestions. Each analysis visits at most 16,384 entries across all searches.
type suggestionSearch struct {
	a         *analyzer
	names     map[string]bool
	remaining int
	exhausted bool
}

func (a *analyzer) newSuggestionSearch() *suggestionSearch {
	return &suggestionSearch{a: a, names: map[string]bool{}, remaining: 2048}
}

func (s *suggestionSearch) visit() bool {
	if s.remaining <= 0 || s.a.suggestionBudget <= 0 {
		s.exhausted = true
		return false
	}
	s.remaining--
	s.a.suggestionBudget--
	return true
}

func (s *suggestionSearch) add(name string, valid bool) bool {
	if !s.visit() {
		return false
	}
	if _, seen := s.names[name]; !seen {
		s.names[name] = valid
	}
	return true
}

func (s *suggestionSearch) hierarchy(name string, visit func(*ClassInfo)) {
	seen := map[string]bool{}
	for name != "" && !seen[name] && !s.exhausted {
		if !s.visit() {
			return
		}
		seen[name] = true
		class := s.a.table.Classes[name]
		if class == nil {
			return
		}
		visit(class)
		name = class.Parent
	}
}

func (s *suggestionSearch) candidate(name string) string {
	if s.exhausted || len(name) < 3 || len(name) > 64 {
		return ""
	}
	best := ""
	for candidate, valid := range s.names {
		if valid && len(candidate) <= 64 && oneEditApart(name, candidate) {
			if best != "" {
				return ""
			} // Ambiguity never produces a preferred fix.
			best = candidate
		}
	}
	return best
}

// COOL identifiers are ASCII. Accept exactly one insertion, deletion,
// substitution or adjacent transposition in linear time, with no DP matrix.
func oneEditApart(a, b string) bool {
	if a == b || len(a)-len(b) > 1 || len(b)-len(a) > 1 {
		return false
	}
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	if len(a) == len(b) {
		return a[i+1:] == b[i+1:] || i+1 < len(a) && a[i] == b[i+1] && a[i+1] == b[i] && a[i+2:] == b[i+2:]
	}
	if len(a) > len(b) {
		return a[i+1:] == b[i:]
	}
	return a[i:] == b[i+1:]
}

func (a *analyzer) attachSuggestion(candidate string, span Span) {
	if candidate == "" || span.StartLine < 1 || span.EndOffset <= span.StartOffset || len(a.diagnostics) == 0 {
		return
	}
	index := len(a.diagnostics) - 1
	a.diagnostics[index] = withQuickFix(a.diagnostics[index], "Replace with "+candidate, candidate, span)
}

func (a *analyzer) suggestVariable(name string, span Span, env *typeEnv, assignment bool) {
	s := a.newSuggestionSearch()
	for i := len(env.scopes) - 1; i >= 0 && !s.exhausted; i-- {
		if !s.visit() {
			break
		}
		for candidate, binding := range env.scopes[i] {
			if !s.add(candidate, a.table.Has(binding.typeName)) {
				break
			}
		}
	}
	s.hierarchy(a.currentClass, func(class *ClassInfo) {
		for candidate, attr := range class.Attributes {
			if !s.add(candidate, a.table.Has(attr.TypeName)) {
				break
			}
		}
	})
	if !assignment {
		s.add("self", true)
	}
	a.attachSuggestion(s.candidate(name), span)
}

func (a *analyzer) suggestMethod(name string, span Span, className string) {
	s := a.newSuggestionSearch()
	s.hierarchy(className, func(class *ClassInfo) {
		for candidate := range class.Methods {
			if !s.add(candidate, true) {
				break
			}
		}
	})
	a.attachSuggestion(s.candidate(name), span)
}

func (a *analyzer) suggestType(name string, span Span, kind, contextClass string) {
	s := a.newSuggestionSearch()
	if kind == "ancestor" {
		s.hierarchy(contextClass, func(class *ClassInfo) { s.add(class.Name, true) })
	} else {
		for candidate := range a.table.Classes {
			valid := kind != "parent" || candidate != typeInt && candidate != typeBool && candidate != typeString && candidate != contextClass
			if !s.add(candidate, valid) {
				break
			}
		}
		if kind == "self" {
			s.add(typeSelf, true)
		}
	}
	candidate := s.candidate(name)
	if kind == "parent" && candidate != "" {
		cycle := false
		s.hierarchy(candidate, func(class *ClassInfo) {
			if class.Name == contextClass {
				cycle = true
			}
		})
		if cycle || s.exhausted {
			return
		}
	}
	a.attachSuggestion(candidate, span)
}
