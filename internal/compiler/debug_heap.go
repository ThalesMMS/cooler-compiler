// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var ErrHeapObjectNotFound = errors.New("heap object not found")

var defaultHeapLimits = HeapLimits{MaxObjects: 100, MaxFields: 64, MaxDepth: 4, StringPreview: 128, MaxBytes: 256 << 10}
var maximumHeapLimits = HeapLimits{MaxObjects: 1_000, MaxFields: 1_000, MaxDepth: 32, StringPreview: 4_096, MaxBytes: 2 << 20}

func DefaultHeapLimits() HeapLimits { return defaultHeapLimits }
func MaximumHeapLimits() HeapLimits { return maximumHeapLimits }

func NormalizeHeapLimits(limits HeapLimits) (HeapLimits, error) {
	fields := []struct {
		name              string
		value             *int
		fallback, maximum int
	}{
		{"maxObjects", &limits.MaxObjects, defaultHeapLimits.MaxObjects, maximumHeapLimits.MaxObjects},
		{"maxFieldsPerObject", &limits.MaxFields, defaultHeapLimits.MaxFields, maximumHeapLimits.MaxFields},
		{"maxDepth", &limits.MaxDepth, defaultHeapLimits.MaxDepth, maximumHeapLimits.MaxDepth},
		{"stringPreview", &limits.StringPreview, defaultHeapLimits.StringPreview, maximumHeapLimits.StringPreview},
		{"maxBytes", &limits.MaxBytes, defaultHeapLimits.MaxBytes, maximumHeapLimits.MaxBytes},
	}
	for _, field := range fields {
		if *field.value == 0 {
			*field.value = field.fallback
		}
		minimum := 1
		if field.name == "maxBytes" {
			minimum = 1_024
		}
		if *field.value < minimum || *field.value > field.maximum {
			return defaultHeapLimits, fmt.Errorf("heap limits %s must be between %d and %d", field.name, minimum, field.maximum)
		}
	}
	return limits, nil
}

// Heap returns a bounded read-only projection of the current VM heap.
func (s *DebugSession) Heap(query HeapQuery) (HeapSnapshot, error) {
	if s.vm == nil {
		return HeapSnapshot{}, fmt.Errorf("debug heap is unavailable")
	}
	return s.vm.heapSnapshot(query)
}

func (vm *VM) heapSnapshot(query HeapQuery) (HeapSnapshot, error) {
	limits, err := NormalizeHeapLimits(query.Limits)
	if err != nil {
		return HeapSnapshot{}, err
	}
	mode := strings.ToLower(strings.TrimSpace(query.Mode))
	if mode == "" {
		mode = "graph"
	}
	if mode != "summary" && mode != "roots" && mode != "object" && mode != "graph" && mode != "search" {
		return HeapSnapshot{}, fmt.Errorf("heap mode must be summary, roots, object, graph, or search")
	}
	if query.Offset < 0 {
		return HeapSnapshot{}, fmt.Errorf("heap offset cannot be negative")
	}

	roots, rootObjects := vm.heapRoots(limits)
	objects := vm.allHeapObjects(rootObjects)
	reachable := reachableHeapObjects(rootObjects)
	snapshot := HeapSnapshot{
		Mode: mode, Objects: []HeapObject{}, Roots: []HeapRoot{}, TotalObjects: len(objects),
		Reachable: len(reachable), Limits: limits,
	}

	switch mode {
	case "summary":
		return finalizeHeapSnapshot(snapshot), nil
	case "roots":
		snapshot.Roots = roots
	case "object":
		object := findHeapObject(objects, query.ID)
		if object == nil {
			return HeapSnapshot{}, fmt.Errorf("%w: %q", ErrHeapObjectNotFound, query.ID)
		}
		snapshot.Objects = []HeapObject{vm.publicHeapObject(object, reachable[object.id], limits)}
	case "search":
		matches := filterHeapObjects(objects, query)
		if query.Offset > len(matches) {
			query.Offset = len(matches)
		}
		end := query.Offset + limits.MaxObjects
		if end > len(matches) {
			end = len(matches)
		}
		for _, object := range matches[query.Offset:end] {
			snapshot.Objects = append(snapshot.Objects, vm.publicHeapObject(object, reachable[object.id], limits))
		}
		if end < len(matches) {
			snapshot.Truncated = true
			snapshot.NextOffset = end
		}
	case "graph":
		starts := rootObjects
		if query.ID != "" {
			object := findHeapObject(objects, query.ID)
			if object == nil {
				return HeapSnapshot{}, fmt.Errorf("%w: %q", ErrHeapObjectNotFound, query.ID)
			}
			starts = []*runtimeObject{object}
		}
		snapshot.Roots = roots
		selected, graphTruncated := traverseHeapGraph(starts, limits)
		for _, object := range selected {
			snapshot.Objects = append(snapshot.Objects, vm.publicHeapObject(object, reachable[object.id], limits))
		}
		snapshot.Truncated = graphTruncated
	}
	markUnavailableHeapReferences(&snapshot)
	return enforceHeapBytes(snapshot), nil
}

func (vm *VM) heapRoots(limits HeapLimits) ([]HeapRoot, []*runtimeObject) {
	roots := []HeapRoot{}
	objects := []*runtimeObject{}
	appendRoot := func(root HeapRoot, value runtimeValueInternal) {
		root.Value = publicHeapValue(value, limits.StringPreview)
		roots = append(roots, root)
		if value.kind == valueObject && value.o != nil {
			objects = append(objects, value.o)
		}
	}
	for frameIndex, frame := range vm.frames {
		frameNumber := frameIndex
		appendRoot(HeapRoot{Kind: "self", Label: fmt.Sprintf("frame[%d].self", frameIndex), Frame: &frameNumber}, frame.self)
		for scopeIndex, scope := range frame.scopes {
			scopeNumber := scopeIndex
			names := make([]string, 0, len(scope))
			for name := range scope {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				appendRoot(HeapRoot{Kind: "local", Label: fmt.Sprintf("frame[%d].scope[%d].%s", frameIndex, scopeIndex, name), Frame: &frameNumber, Scope: &scopeNumber}, scope[name])
			}
		}
	}
	for index, value := range vm.stack {
		stackIndex := index
		appendRoot(HeapRoot{Kind: "stack", Label: fmt.Sprintf("stack[%d]", index), StackIndex: &stackIndex}, value)
	}
	return roots, objects
}

func (vm *VM) allHeapObjects(rootObjects []*runtimeObject) []*runtimeObject {
	byID := map[int]*runtimeObject{}
	for id, object := range vm.objects {
		if object != nil {
			byID[id] = object
		}
	}
	queue := append([]*runtimeObject(nil), rootObjects...)
	for len(queue) > 0 {
		object := queue[0]
		queue = queue[1:]
		if object == nil || byID[object.id] != nil {
			continue
		}
		byID[object.id] = object
		for _, value := range object.fields {
			if value.kind == valueObject && value.o != nil {
				queue = append(queue, value.o)
			}
		}
	}
	result := make([]*runtimeObject, 0, len(byID))
	for _, object := range byID {
		result = append(result, object)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result
}

func reachableHeapObjects(starts []*runtimeObject) map[int]bool {
	reachable := map[int]bool{}
	queue := append([]*runtimeObject(nil), starts...)
	for len(queue) > 0 {
		object := queue[0]
		queue = queue[1:]
		if object == nil || reachable[object.id] {
			continue
		}
		reachable[object.id] = true
		for _, value := range object.fields {
			if value.kind == valueObject && value.o != nil {
				queue = append(queue, value.o)
			}
		}
	}
	return reachable
}

func traverseHeapGraph(starts []*runtimeObject, limits HeapLimits) ([]*runtimeObject, bool) {
	type queued struct {
		object *runtimeObject
		depth  int
	}
	queue := make([]queued, 0, len(starts))
	for _, object := range starts {
		queue = append(queue, queued{object: object})
	}
	seen := map[int]bool{}
	result := []*runtimeObject{}
	truncated := false
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.object == nil || seen[current.object.id] {
			continue
		}
		if len(result) >= limits.MaxObjects {
			truncated = true
			break
		}
		seen[current.object.id] = true
		result = append(result, current.object)
		if current.depth >= limits.MaxDepth {
			for _, value := range current.object.fields {
				if value.kind == valueObject && value.o != nil && !seen[value.o.id] {
					truncated = true
				}
			}
			continue
		}
		references := objectReferences(current.object)
		for _, reference := range references {
			queue = append(queue, queued{object: reference, depth: current.depth + 1})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result, truncated
}

func objectReferences(object *runtimeObject) []*runtimeObject {
	result := []*runtimeObject{}
	for _, value := range object.fields {
		if value.kind == valueObject && value.o != nil {
			result = append(result, value.o)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result
}

func (vm *VM) publicHeapObject(object *runtimeObject, reachable bool, limits HeapLimits) HeapObject {
	result := HeapObject{
		ID: heapObjectID(object), Class: object.class, AllocationOrder: object.id,
		AllocationSpan: object.allocationSpan, Reachable: reachable,
		Fields: []HeapField{}, Summary: fmt.Sprintf("%s with %d fields", object.class, len(object.fields)),
	}
	attributes := []runtimeAttribute{}
	if vm.exec != nil {
		if class := vm.exec.Classes[object.class]; class != nil {
			attributes = append(attributes, class.Attributes...)
		}
	}
	known := map[string]bool{}
	for _, attribute := range attributes {
		known[attribute.Name] = true
		result.Fields = append(result.Fields, HeapField{
			Name: attribute.Name, Owner: attribute.Owner, StaticType: attribute.TypeName, Span: attribute.Span,
			Value: publicHeapValue(object.fields[attribute.Name], limits.StringPreview),
		})
	}
	extra := []string{}
	for name := range object.fields {
		if !known[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		result.Fields = append(result.Fields, HeapField{Name: name, Owner: object.class, Value: publicHeapValue(object.fields[name], limits.StringPreview)})
	}
	if len(result.Fields) > limits.MaxFields {
		result.OmittedFields = len(result.Fields) - limits.MaxFields
		result.Fields = result.Fields[:limits.MaxFields]
		result.Truncated = true
	}
	return result
}

func publicHeapValue(value runtimeValueInternal, previewLimit int) HeapValue {
	result := HeapValue{Type: value.typeName()}
	switch value.kind {
	case valueVoid:
		result.Kind, result.Type, result.Display = "void", "Void", "void"
	case valueInt:
		result.Kind, result.Display = "int", value.i.String()
	case valueBool:
		result.Kind, result.Display = "bool", strconv.FormatBool(value.b)
	case valueString:
		result.Kind = "string"
		runes := []rune(value.s)
		result.FullLength = len(runes)
		if len(runes) > previewLimit {
			runes = runes[:previewLimit]
			result.Truncated = true
		}
		result.Display = strconv.Quote(string(runes))
	case valueObject:
		if value.o == nil {
			result.Kind, result.Type, result.Display = "void", "Void", "void"
		} else {
			result.Kind, result.ObjectID, result.Display = "reference", heapObjectID(value.o), heapObjectID(value.o)
		}
	}
	return result
}

func heapObjectID(object *runtimeObject) string {
	if object == nil {
		return ""
	}
	return fmt.Sprintf("%s#%d", object.class, object.id)
}

func findHeapObject(objects []*runtimeObject, id string) *runtimeObject {
	for _, object := range objects {
		if heapObjectID(object) == id || strconv.Itoa(object.id) == id {
			return object
		}
	}
	return nil
}

func filterHeapObjects(objects []*runtimeObject, query HeapQuery) []*runtimeObject {
	result := []*runtimeObject{}
	for _, object := range objects {
		if query.Class != "" && object.class != query.Class {
			continue
		}
		if query.ID != "" && !strings.Contains(heapObjectID(object), query.ID) {
			continue
		}
		result = append(result, object)
	}
	return result
}

func markUnavailableHeapReferences(snapshot *HeapSnapshot) {
	available := map[string]bool{}
	for _, object := range snapshot.Objects {
		available[object.ID] = true
	}
	mark := func(value *HeapValue) {
		if value.Kind == "reference" && !available[value.ObjectID] {
			value.Unavailable = true
		}
	}
	for index := range snapshot.Roots {
		mark(&snapshot.Roots[index].Value)
	}
	for objectIndex := range snapshot.Objects {
		for fieldIndex := range snapshot.Objects[objectIndex].Fields {
			mark(&snapshot.Objects[objectIndex].Fields[fieldIndex].Value)
		}
	}
}

func finalizeHeapSnapshot(snapshot HeapSnapshot) HeapSnapshot {
	encoded, _ := json.Marshal(snapshot)
	snapshot.Serialized = len(encoded)
	encoded, _ = json.Marshal(snapshot)
	snapshot.Serialized = len(encoded)
	return snapshot
}

func enforceHeapBytes(snapshot HeapSnapshot) HeapSnapshot {
	for {
		snapshot = finalizeHeapSnapshot(snapshot)
		if snapshot.Serialized <= snapshot.Limits.MaxBytes {
			return snapshot
		}
		snapshot.Truncated = true
		excess := snapshot.Serialized - snapshot.Limits.MaxBytes
		if len(snapshot.Objects) > 0 {
			last := len(snapshot.Objects) - 1
			if len(snapshot.Objects[last].Fields) > 0 {
				fields := snapshot.Objects[last].Fields
				remove := proportionalHeapRemoval(len(fields), excess, snapshot.Serialized)
				for index := len(fields) - remove; index < len(fields); index++ {
					fields[index] = HeapField{}
				}
				snapshot.Objects[last].Fields = fields[:len(fields)-remove]
				snapshot.Objects[last].OmittedFields += remove
				snapshot.Objects[last].Truncated = true
			} else {
				remove := proportionalHeapRemoval(len(snapshot.Objects), excess, snapshot.Serialized)
				for index := len(snapshot.Objects) - remove; index < len(snapshot.Objects); index++ {
					snapshot.Objects[index] = HeapObject{}
				}
				snapshot.Objects = snapshot.Objects[:len(snapshot.Objects)-remove]
			}
		} else if len(snapshot.Roots) > 0 {
			remove := proportionalHeapRemoval(len(snapshot.Roots), excess, snapshot.Serialized)
			for index := len(snapshot.Roots) - remove; index < len(snapshot.Roots); index++ {
				snapshot.Roots[index] = HeapRoot{}
			}
			snapshot.Roots = snapshot.Roots[:len(snapshot.Roots)-remove]
		} else {
			return snapshot
		}
		markUnavailableHeapReferences(&snapshot)
	}
}

func proportionalHeapRemoval(count, excess, serialized int) int {
	if count <= 1 || excess <= 0 || serialized <= 0 {
		return 1
	}
	remove := int((int64(excess)*int64(count) + int64(serialized) - 1) / int64(serialized))
	if remove < 1 {
		return 1
	}
	if remove > count {
		return count
	}
	return remove
}
