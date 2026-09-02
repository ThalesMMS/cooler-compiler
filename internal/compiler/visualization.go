// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"sort"
	"strings"
)

const visualizationSchemaVersion = "1.0.0"

var defaultVisualizationLimits = VisualizationLimits{
	MaxClasses: 512, MaxArtifacts: 12_000, MaxGroups: 3_000, MaxRelations: 50_000,
}

// BuildVisualizations materializes the public, bounded relationship model.
// It deliberately consumes analyzer structures rather than reconstructing
// semantic facts from formatted symbol or IR strings.
func BuildVisualizations(program *Program, analysis Analysis, result Result) *VisualizationBundle {
	b := &visualizationBuilder{
		program: program, analysis: analysis, result: result,
		bundle: VisualizationBundle{
			SchemaVersion:   visualizationSchemaVersion,
			CompilerVersion: result.CompilerVersion,
			Model:           "Cooler logical educational object and dispatch model; not a native or reference ABI",
			Limits:          defaultVisualizationLimits,
		},
	}
	for _, unit := range result.SourceUnits {
		b.bundle.SourceUnitIDs = append(b.bundle.SourceUnitIDs, unit.ID)
	}
	b.buildInheritance()
	b.buildClassModels()
	b.buildProvenance()
	return &b.bundle
}

type visualizationBuilder struct {
	program  *Program
	analysis Analysis
	result   Result
	bundle   VisualizationBundle
}

func (b *visualizationBuilder) buildInheritance() {
	if b.analysis.Table == nil {
		return
	}
	invalidByClass := map[string][]string{}
	for _, diagnostic := range b.result.Diagnostics {
		if diagnostic.Stage != "semantic" || diagnostic.Severity != SeverityError {
			continue
		}
		for _, class := range b.programClasses() {
			if spansOverlap(class.Span, diagnostic.Span) || spansOverlap(class.ParentSpan, diagnostic.Span) {
				invalidByClass[class.Name] = appendUnique(invalidByClass[class.Name], diagnostic.Code)
			}
		}
	}
	originalParents := map[string]string{}
	parentSpans := map[string]Span{}
	for _, class := range b.programClasses() {
		if _, exists := originalParents[class.Name]; exists {
			continue
		}
		parent := class.Parent
		if parent == "" {
			parent = typeObject
		}
		originalParents[class.Name] = parent
		parentSpans[class.Name] = class.ParentSpan
	}
	cycleMembers := originalCycleMembers(originalParents)

	for _, name := range b.analysis.Table.Order {
		if len(b.bundle.Inheritance) >= b.bundle.Limits.MaxClasses {
			b.bundle.Truncated = true
			break
		}
		class := b.analysis.Table.Classes[name]
		if class == nil {
			continue
		}
		parent := class.Parent
		span := class.Span
		kind, status := "user", "valid"
		if class.Builtin {
			kind, status = "basic", "valid"
		} else {
			if original, ok := originalParents[name]; ok {
				parent = original
			}
			if len(invalidByClass[name]) > 0 || cycleMembers[name] || parent != class.Parent {
				status = "invalid-recovered"
			}
		}
		node := InheritanceNode{
			ID: "class:" + name, Name: name, Parent: parent, Kind: kind, Status: status,
			Span: span, Diagnostics: append([]string(nil), invalidByClass[name]...),
		}
		if parent != class.Parent {
			node.RecoveredParent = class.Parent
		}
		b.bundle.Inheritance = append(b.bundle.Inheritance, node)
		if parent != "" {
			edgeSpan := parentSpans[name]
			if edgeSpan.StartLine == 0 {
				edgeSpan = span
			}
			edge := InheritanceEdge{From: name, To: parent, Status: status, Span: edgeSpan, Cycle: cycleMembers[name] && cycleMembers[parent]}
			if parent != class.Parent {
				edge.Recovered = true
			}
			b.bundle.Edges = append(b.bundle.Edges, edge)
		}
	}
	sort.SliceStable(b.bundle.Edges, func(i, j int) bool {
		if b.bundle.Edges[i].To != b.bundle.Edges[j].To {
			return b.bundle.Edges[i].To < b.bundle.Edges[j].To
		}
		return b.bundle.Edges[i].From < b.bundle.Edges[j].From
	})
}

func (b *visualizationBuilder) buildClassModels() {
	if b.analysis.Table == nil {
		return
	}
	status := map[string]string{}
	for _, node := range b.bundle.Inheritance {
		status[node.Name] = node.Status
	}
	for _, name := range b.analysis.Table.Order {
		if len(b.bundle.Classes) >= b.bundle.Limits.MaxClasses {
			b.bundle.Truncated = true
			break
		}
		class := b.analysis.Table.Classes[name]
		if class == nil {
			continue
		}
		lineage := classLineage(b.analysis.Table, name)
		model := LogicalClassModel{
			Class: name, Parent: class.Parent, Status: status[name], Span: class.Span,
			Attributes: []LogicalAttribute{}, Dispatch: []DispatchSlot{},
		}
		for index := len(lineage) - 1; index >= 0; index-- {
			model.LookupOrder = append(model.LookupOrder, lineage[index])
		}
		for _, ownerName := range reverseStrings(lineage) {
			owner := b.analysis.Table.Classes[ownerName]
			if owner == nil {
				continue
			}
			for _, attr := range orderedAttributes(owner) {
				model.Attributes = append(model.Attributes, LogicalAttribute{
					ID: "attribute:" + attr.Owner + "." + attr.Name, Name: attr.Name,
					Type: attr.TypeName, Owner: attr.Owner, Inherited: attr.Owner != name,
					Slot: len(model.Attributes), Builtin: attr.Builtin, Span: attr.Span,
				})
			}
		}
		slotByName := map[string]int{}
		for _, ownerName := range reverseStrings(lineage) {
			owner := b.analysis.Table.Classes[ownerName]
			if owner == nil {
				continue
			}
			for _, methodName := range orderedMethodNames(owner) {
				method := owner.Methods[methodName]
				entry := DispatchSlot{
					ID: "method:" + method.Owner + "." + method.Name, Name: method.Name,
					Signature: methodSignature(method), Owner: method.Owner,
					Inherited: method.Owner != name, ReturnsSelfType: method.ReturnType == typeSelf,
					LookupStarts: append([]string(nil), lineage...), Builtin: method.Builtin, Span: method.Span,
				}
				if slot, exists := slotByName[method.Name]; exists {
					entry.Slot = slot
					entry.Overrides = model.Dispatch[slot].Owner + "." + method.Name
					model.Dispatch[slot] = entry
				} else {
					entry.Slot = len(model.Dispatch)
					slotByName[method.Name] = entry.Slot
					model.Dispatch = append(model.Dispatch, entry)
				}
			}
		}
		b.bundle.Classes = append(b.bundle.Classes, model)
	}
}

func (b *visualizationBuilder) buildProvenance() {
	add := func(phase, kind, label string, span Span, synthetic bool) int {
		if len(b.bundle.Artifacts) >= b.bundle.Limits.MaxArtifacts {
			b.bundle.Truncated = true
			return -1
		}
		index := len(b.bundle.Artifacts)
		b.bundle.Artifacts = append(b.bundle.Artifacts, ProvenanceArtifact{
			ID: fmt.Sprintf("%s:%06d", phase, index), Phase: phase, Kind: kind,
			Label: label, Span: span, Synthetic: synthetic || span.SourceID == "",
		})
		return index
	}
	for _, token := range b.result.Tokens {
		add("tokens", string(token.Type), token.Lexeme, token.Span, token.Type == TokenEOF)
	}
	var walkAST func(ASTNode)
	walkAST = func(node ASTNode) {
		index := add("ast", node.Kind, firstNonEmpty(node.Text, node.Name, node.Kind), node.Span, false)
		if index >= 0 && len(b.bundle.Groups) < b.bundle.Limits.MaxGroups {
			groupIndex := len(b.bundle.Groups)
			artifact := b.bundle.Artifacts[index]
			b.bundle.Groups = append(b.bundle.Groups, ProvenanceGroup{
				ID: fmt.Sprintf("expression:%06d", groupIndex), SourceID: node.Span.SourceID,
				Span: node.Span, ASTKind: node.Kind, StaticType: node.Type,
				ArtifactIDs: []string{artifact.ID},
			})
		} else if index >= 0 {
			b.bundle.Truncated = true
		}
		for _, child := range node.Children {
			walkAST(child)
		}
	}
	if b.result.AST != nil {
		walkAST(*b.result.AST)
	}
	for _, symbol := range b.result.Symbols {
		add("symbols", symbol.Kind, symbol.Owner+"."+symbol.Name, symbol.Span, symbol.Span.SourceID == "")
	}
	for _, instruction := range b.result.IR {
		add("ir", instruction.Op, fmt.Sprintf("%d %s %s", instruction.Index, instruction.Method, instruction.Op), instruction.Span, instruction.Synthetic)
	}
	for _, block := range b.result.ControlFlow.Blocks {
		add("cfg", "block", block.Name, block.Span, block.Span.SourceID == "")
	}
	for _, edge := range b.result.ControlFlow.Edges {
		add("cfg", "edge", edge.From+" "+edge.Kind+" "+edge.To, edge.Span, edge.Span.SourceID == "")
	}
	for index, line := range b.result.Assembly {
		span := Span{}
		if index < len(b.result.AssemblySpans) {
			span = b.result.AssemblySpans[index]
		}
		add("assembly", "line", strings.TrimSpace(line), span, span.SourceID == "")
	}
	for _, state := range b.result.Runtime.Trace {
		add("vm", state.Op, fmt.Sprintf("step %d %s", state.Step, state.Op), state.Span, state.Span.SourceID == "")
	}
	for _, diagnostic := range b.result.Diagnostics {
		add("diagnostics", diagnostic.Code, diagnostic.Message, diagnostic.Span, false)
	}

	relations := len(b.bundle.Groups)
	work := 0
	for groupIndex := range b.bundle.Groups {
		group := &b.bundle.Groups[groupIndex]
		owned := make(map[string]struct{}, len(group.ArtifactIDs))
		for _, id := range group.ArtifactIDs {
			owned[id] = struct{}{}
		}
		for artifactIndex := range b.bundle.Artifacts {
			work++
			if work > 2_000_000 || relations >= b.bundle.Limits.MaxRelations {
				b.bundle.Truncated = true
				return
			}
			artifact := b.bundle.Artifacts[artifactIndex]
			if _, exists := owned[artifact.ID]; artifact.Phase == "ast" && exists {
				continue
			}
			if spansOverlap(group.Span, artifact.Span) {
				group.ArtifactIDs = append(group.ArtifactIDs, artifact.ID)
				owned[artifact.ID] = struct{}{}
				relations++
			}
		}
	}
}

func (b *visualizationBuilder) programClasses() []*ClassDecl {
	if b.program == nil {
		return nil
	}
	return b.program.Classes
}

func classLineage(table *ClassTable, name string) []string {
	lineage, seen := []string{}, map[string]bool{}
	for name != "" && !seen[name] {
		seen[name] = true
		lineage = append(lineage, name)
		class := table.Classes[name]
		if class == nil {
			break
		}
		name = class.Parent
	}
	return lineage
}

func reverseStrings(values []string) []string {
	result := make([]string, len(values))
	for index := range values {
		result[len(values)-1-index] = values[index]
	}
	return result
}

func originalCycleMembers(parents map[string]string) map[string]bool {
	result := map[string]bool{}
	for start := range parents {
		path, positions := []string{}, map[string]int{}
		current := start
		for current != "" {
			if at, found := positions[current]; found {
				for _, member := range path[at:] {
					result[member] = true
				}
				break
			}
			positions[current] = len(path)
			path = append(path, current)
			current = parents[current]
		}
	}
	return result
}

func spansOverlap(left, right Span) bool {
	if left.SourceID == "" || right.SourceID == "" || left.SourceID != right.SourceID {
		return false
	}
	return left.StartOffset <= right.EndOffset && right.StartOffset <= left.EndOffset
}

func appendUnique(values []string, value string) []string {
	if value == "" || containsString(values, value) {
		return values
	}
	return append(values, value)
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "artifact"
}

func cloneVisualizations(source *VisualizationBundle) *VisualizationBundle {
	if source == nil {
		return nil
	}
	result := *source
	result.SourceUnitIDs = append([]string{}, source.SourceUnitIDs...)
	result.Inheritance = append([]InheritanceNode{}, source.Inheritance...)
	for index := range result.Inheritance {
		result.Inheritance[index].Diagnostics = append([]string(nil), source.Inheritance[index].Diagnostics...)
	}
	result.Edges = append([]InheritanceEdge{}, source.Edges...)
	result.Classes = append([]LogicalClassModel{}, source.Classes...)
	for index := range result.Classes {
		result.Classes[index].Attributes = append([]LogicalAttribute{}, source.Classes[index].Attributes...)
		result.Classes[index].Dispatch = append([]DispatchSlot{}, source.Classes[index].Dispatch...)
		for slot := range result.Classes[index].Dispatch {
			result.Classes[index].Dispatch[slot].LookupStarts = append([]string(nil), source.Classes[index].Dispatch[slot].LookupStarts...)
		}
		result.Classes[index].LookupOrder = append([]string{}, source.Classes[index].LookupOrder...)
	}
	result.Artifacts = append([]ProvenanceArtifact{}, source.Artifacts...)
	result.Groups = append([]ProvenanceGroup{}, source.Groups...)
	for index := range result.Groups {
		result.Groups[index].ArtifactIDs = append([]string(nil), source.Groups[index].ArtifactIDs...)
	}
	return &result
}
