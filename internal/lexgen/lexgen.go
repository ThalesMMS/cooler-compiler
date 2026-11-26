// SPDX-License-Identifier: AGPL-3.0-or-later

// Package lexgen is Cooler's build-time lexer generator: a small Flex-like
// tool that compiles a readable specification (regular expressions grouped
// by start condition, each with a Go action) into DFA transition tables and
// an action switch. It runs only on development machines through
// `make generate-frontend` (cmd/cooler-lexgen); the generated Go source is
// committed, and nothing is generated at build time or on a device.
//
// Specification format (see internal/compiler/cool.lex):
//
//	%package NAME
//	%receiver NAME TYPE            // receiver of the generated methods
//	%states A B C                  // start conditions; the first is initial
//	%%
//	<A,B>REGEX  { Go statements }   // ordinary rule
//	<A><<EOF>>  { Go statements }   // end of input in condition A
//	<A><<INVALID>> { ... }          // malformed UTF-8 byte at a token start
//
// Matching follows Flex: the longest match wins, and among equally long
// matches the earliest rule wins. Regular expressions use Go regexp syntax
// without anchors; a pattern ends at the first unbracketed, unescaped blank.
package lexgen

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"regexp/syntax"
	"sort"
	"strings"
	"unicode"
)

const maxRune = unicode.MaxRune

type rule struct {
	states  []string
	pattern string
	special string // "", "EOF", or "INVALID"
	action  string
	line    int
	index   int
}

type spec struct {
	pkg, receiver, receiverType string
	states                      []string
	rules                       []rule
}

// Generate compiles a specification into gofmt-formatted Go source.
func Generate(source []byte, origin string) ([]byte, error) {
	parsed, err := parseSpec(source)
	if err != nil {
		return nil, err
	}
	return generate(parsed, origin)
}

func parseSpec(source []byte) (spec, error) {
	var result spec
	scanner := bufio.NewScanner(bytes.NewReader(source))
	scanner.Buffer(make([]byte, 1<<16), 1<<20)
	inRules := false
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimRight(scanner.Text(), " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if !inRules {
			fields := strings.Fields(trimmed)
			switch fields[0] {
			case "%package":
				result.pkg = fields[1]
			case "%receiver":
				if len(fields) != 3 {
					return result, fmt.Errorf("line %d: %%receiver needs NAME TYPE", lineNumber)
				}
				result.receiver, result.receiverType = fields[1], fields[2]
			case "%states":
				result.states = fields[1:]
			case "%%":
				inRules = true
			default:
				return result, fmt.Errorf("line %d: unknown directive %q", lineNumber, fields[0])
			}
			continue
		}
		parsedRule, err := parseRule(trimmed, lineNumber)
		if err != nil {
			return result, err
		}
		parsedRule.index = len(result.rules)
		result.rules = append(result.rules, parsedRule)
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	if result.pkg == "" || result.receiver == "" || len(result.states) == 0 || len(result.rules) == 0 {
		return result, errors.New("specification needs %package, %receiver, %states, and rules")
	}
	known := map[string]bool{}
	for _, state := range result.states {
		known[state] = true
	}
	for _, r := range result.rules {
		for _, state := range r.states {
			if !known[state] {
				return result, fmt.Errorf("line %d: unknown start condition %q", r.line, state)
			}
		}
	}
	return result, nil
}

func parseRule(line string, lineNumber int) (rule, error) {
	result := rule{line: lineNumber}
	if !strings.HasPrefix(line, "<") {
		return result, fmt.Errorf("line %d: rules start with <STATE,...>", lineNumber)
	}
	end := strings.Index(line, ">")
	if end < 0 {
		return result, fmt.Errorf("line %d: unterminated start-condition list", lineNumber)
	}
	result.states = strings.Split(line[1:end], ",")
	rest := line[end+1:]
	for _, special := range []string{"EOF", "INVALID"} {
		if strings.HasPrefix(rest, "<<"+special+">>") {
			result.special = special
			rest = rest[len(special)+4:]
		}
	}
	if result.special == "" {
		inClass, escaped := false, false
		cut := -1
		for index, char := range rest {
			switch {
			case escaped:
				escaped = false
			case char == '\\':
				escaped = true
			case char == '[':
				inClass = true
			case char == ']':
				inClass = false
			case (char == ' ' || char == '\t') && !inClass:
				cut = index
			}
			if cut >= 0 {
				break
			}
		}
		if cut < 0 {
			return result, fmt.Errorf("line %d: rule has no action", lineNumber)
		}
		result.pattern, rest = rest[:cut], rest[cut:]
	}
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, "{") || !strings.HasSuffix(rest, "}") {
		return result, fmt.Errorf("line %d: actions are written as { Go statements }", lineNumber)
	}
	result.action = strings.TrimSpace(rest[1 : len(rest)-1])
	return result, nil
}

// nfa is a Thompson automaton over rune ranges.
type nfa struct {
	edges   [][]nfaEdge
	epsilon [][]int
	accept  map[int]int // state -> rule index
}

type nfaEdge struct {
	lo, hi rune
	to     int
}

func (n *nfa) state() int {
	n.edges = append(n.edges, nil)
	n.epsilon = append(n.epsilon, nil)
	return len(n.edges) - 1
}

func (n *nfa) build(re *syntax.Regexp) (int, int, error) {
	start, end := n.state(), n.state()
	switch re.Op {
	case syntax.OpEmptyMatch:
		n.epsilon[start] = append(n.epsilon[start], end)
	case syntax.OpLiteral:
		current := start
		for index, r := range re.Rune {
			next := end
			if index < len(re.Rune)-1 {
				next = n.state()
			}
			for _, folded := range foldRune(r, re.Flags&syntax.FoldCase != 0) {
				n.edges[current] = append(n.edges[current], nfaEdge{folded, folded, next})
			}
			current = next
		}
		if len(re.Rune) == 0 {
			n.epsilon[start] = append(n.epsilon[start], end)
		}
	case syntax.OpCharClass:
		for index := 0; index+1 < len(re.Rune); index += 2 {
			n.edges[start] = append(n.edges[start], nfaEdge{re.Rune[index], re.Rune[index+1], end})
		}
	case syntax.OpAnyCharNotNL:
		n.edges[start] = append(n.edges[start], nfaEdge{0, '\n' - 1, end}, nfaEdge{'\n' + 1, maxRune, end})
	case syntax.OpAnyChar:
		n.edges[start] = append(n.edges[start], nfaEdge{0, maxRune, end})
	case syntax.OpCapture:
		s, e, err := n.build(re.Sub[0])
		if err != nil {
			return 0, 0, err
		}
		n.epsilon[start] = append(n.epsilon[start], s)
		n.epsilon[e] = append(n.epsilon[e], end)
	case syntax.OpConcat:
		current := start
		for _, sub := range re.Sub {
			s, e, err := n.build(sub)
			if err != nil {
				return 0, 0, err
			}
			n.epsilon[current] = append(n.epsilon[current], s)
			current = e
		}
		n.epsilon[current] = append(n.epsilon[current], end)
	case syntax.OpAlternate:
		for _, sub := range re.Sub {
			s, e, err := n.build(sub)
			if err != nil {
				return 0, 0, err
			}
			n.epsilon[start] = append(n.epsilon[start], s)
			n.epsilon[e] = append(n.epsilon[e], end)
		}
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest:
		s, e, err := n.build(re.Sub[0])
		if err != nil {
			return 0, 0, err
		}
		n.epsilon[start] = append(n.epsilon[start], s)
		n.epsilon[e] = append(n.epsilon[e], end)
		if re.Op != syntax.OpPlus {
			n.epsilon[start] = append(n.epsilon[start], end)
		}
		if re.Op != syntax.OpQuest {
			n.epsilon[e] = append(n.epsilon[e], s)
		}
	default:
		return 0, 0, fmt.Errorf("unsupported regular-expression operator %v", re.Op)
	}
	return start, end, nil
}

// foldRune expands a case-insensitive literal rune. Like Flex's
// case-insensitive patterns, folding is ASCII-only: (?i:s) must not match
// U+017F, and (?i:k) must not match the Kelvin sign.
func foldRune(r rune, fold bool) []rune {
	switch {
	case !fold:
		return []rune{r}
	case r >= 'a' && r <= 'z':
		return []rune{r, r - 'a' + 'A'}
	case r >= 'A' && r <= 'Z':
		return []rune{r, r - 'A' + 'a'}
	}
	return []rune{r}
}

type dfa struct {
	start  map[string]int
	delta  [][]int // state -> class -> next (-1 none)
	accept []int   // state -> rule (-1 none)
}

func generate(s spec, origin string) ([]byte, error) {
	machine := &nfa{accept: map[int]int{}}
	starts := map[string][]int{}
	for _, r := range s.rules {
		if r.special != "" {
			continue
		}
		re, err := syntax.Parse(r.pattern, syntax.Perl)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", r.line, err)
		}
		begin, end, err := machine.build(re.Simplify())
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", r.line, err)
		}
		machine.accept[end] = r.index
		for _, state := range r.states {
			starts[state] = append(starts[state], begin)
		}
	}
	bounds := classBounds(machine)
	automaton := subsetConstruction(machine, s.states, starts, bounds)
	return emit(s, automaton, bounds, origin)
}

// classBounds partitions the rune space into intervals on which every NFA
// edge is either fully defined or undefined.
func classBounds(machine *nfa) []rune {
	cuts := map[rune]bool{0: true}
	for _, edges := range machine.edges {
		for _, edge := range edges {
			cuts[edge.lo] = true
			if edge.hi < maxRune {
				cuts[edge.hi+1] = true
			}
		}
	}
	bounds := make([]rune, 0, len(cuts))
	for cut := range cuts {
		bounds = append(bounds, cut)
	}
	sort.Slice(bounds, func(i, j int) bool { return bounds[i] < bounds[j] })
	return bounds
}

func (n *nfa) closure(states []int) []int {
	seen := map[int]bool{}
	stack := append([]int(nil), states...)
	for len(stack) > 0 {
		state := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[state] {
			continue
		}
		seen[state] = true
		stack = append(stack, n.epsilon[state]...)
	}
	result := make([]int, 0, len(seen))
	for state := range seen {
		result = append(result, state)
	}
	sort.Ints(result)
	return result
}

func subsetConstruction(machine *nfa, conditions []string, starts map[string][]int, bounds []rune) dfa {
	result := dfa{start: map[string]int{}}
	index := map[string]int{}
	var queue [][]int
	add := func(set []int) int {
		key := fmt.Sprint(set)
		if id, ok := index[key]; ok {
			return id
		}
		id := len(result.delta)
		index[key] = id
		result.delta = append(result.delta, nil)
		accept := -1
		for _, state := range set {
			if r, ok := machine.accept[state]; ok && (accept < 0 || r < accept) {
				accept = r
			}
		}
		result.accept = append(result.accept, accept)
		queue = append(queue, set)
		return id
	}
	for _, condition := range conditions {
		result.start[condition] = add(machine.closure(starts[condition]))
	}
	for processed := 0; processed < len(queue); processed++ {
		set := queue[processed]
		row := make([]int, len(bounds))
		for class, lo := range bounds {
			var next []int
			for _, state := range set {
				for _, edge := range machine.edges[state] {
					if edge.lo <= lo && lo <= edge.hi {
						next = append(next, edge.to)
					}
				}
			}
			if len(next) == 0 {
				row[class] = -1
				continue
			}
			row[class] = add(machine.closure(next))
		}
		result.delta[processed] = row
	}
	return result
}

func emit(s spec, automaton dfa, bounds []rune, origin string) ([]byte, error) {
	var out bytes.Buffer
	fmt.Fprintf(&out, "// Code generated by cooler-lexgen from %s. DO NOT EDIT.\n\n", origin)
	fmt.Fprintf(&out, "package %s\n\n", s.pkg)
	out.WriteString("// Start conditions.\nconst (\n")
	for index, state := range s.states {
		fmt.Fprintf(&out, "\tgenlex%s = %d\n", state, index)
	}
	out.WriteString(")\n\n")
	fmt.Fprintf(&out, "// genlexStart is the DFA start state of each start condition.\nvar genlexStart = [...]int{")
	for _, state := range s.states {
		fmt.Fprintf(&out, "%d, ", automaton.start[state])
	}
	out.WriteString("}\n\n")
	out.WriteString("// genlexBounds partitions the runes into character classes: class i\n// holds the runes from genlexBounds[i] up to genlexBounds[i+1]-1.\nvar genlexBounds = []rune{")
	for index, bound := range bounds {
		if index%12 == 0 {
			out.WriteString("\n\t")
		}
		fmt.Fprintf(&out, "%d, ", bound)
	}
	out.WriteString("\n}\n\n")
	fmt.Fprintf(&out, "const genlexClasses = %d\n\n", len(bounds))
	out.WriteString("// genlexDelta[state*genlexClasses+class] is the next state, or -1.\nvar genlexDelta = []int16{")
	count := 0
	for _, row := range automaton.delta {
		for _, next := range row {
			if count%20 == 0 {
				out.WriteString("\n\t")
			}
			fmt.Fprintf(&out, "%d, ", next)
			count++
		}
	}
	out.WriteString("\n}\n\n")
	out.WriteString("// genlexAccept[state] is the rule recognized in state, or -1.\nvar genlexAccept = []int16{")
	for index, rule := range automaton.accept {
		if index%20 == 0 {
			out.WriteString("\n\t")
		}
		fmt.Fprintf(&out, "%d, ", rule)
	}
	out.WriteString("\n}\n\n")

	receiver := s.receiver + " " + s.receiverType
	fmt.Fprintf(&out, "// genlexAction runs the action of an ordinary rule.\nfunc (%s) genlexAction(rule int) {\n\tswitch rule {\n", receiver)
	for _, r := range s.rules {
		if r.special != "" {
			continue
		}
		fmt.Fprintf(&out, "\tcase %d: // line %d: <%s>%s\n\t\t%s\n", r.index, r.line, strings.Join(r.states, ","), r.pattern, r.action)
	}
	out.WriteString("\t}\n}\n\n")
	for _, special := range []string{"EOF", "INVALID"} {
		name := map[string]string{"EOF": "Eof", "INVALID": "Invalid"}[special]
		fmt.Fprintf(&out, "// genlex%sAction runs the <<%s>> rule of the current start condition\n// and reports whether one exists.\nfunc (%s) genlex%sAction(condition int) bool {\n\tswitch condition {\n", name, special, receiver, name)
		for _, r := range s.rules {
			if r.special != special {
				continue
			}
			cases := make([]string, len(r.states))
			for index, state := range r.states {
				cases[index] = "genlex" + state
			}
			fmt.Fprintf(&out, "\tcase %s: // line %d\n\t\t%s\n\t\treturn true\n", strings.Join(cases, ", "), r.line, r.action)
		}
		out.WriteString("\t}\n\treturn false\n}\n\n")
	}
	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated lexer: %w\n%s", err, out.String())
	}
	return formatted, nil
}
