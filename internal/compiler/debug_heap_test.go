// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestHeapSnapshotIsDeterministicCycleSafeAndExplicit(t *testing.T) {
	parentSpan := Span{SourceID: "model", StartLine: 1, StartColumn: 1, EndLine: 1, EndColumn: 2}
	childSpan := Span{SourceID: "model", StartLine: 2, StartColumn: 1, EndLine: 2, EndColumn: 2}
	class := &runtimeClass{Name: "Child", Parent: "Parent", Attributes: []runtimeAttribute{
		{Name: "inherited", TypeName: "Int", Owner: "Parent", Span: parentSpan},
		{Name: "selfref", TypeName: "Child", Owner: "Child", Span: childSpan},
		{Name: "peer", TypeName: "Child", Owner: "Child", Span: childSpan},
		{Name: "shared", TypeName: "Object", Owner: "Child", Span: childSpan},
		{Name: "text", TypeName: "String", Owner: "Child", Span: childSpan},
	}}
	first := &runtimeObject{id: 1, class: "Child", fields: map[string]runtimeValueInternal{}, allocationSpan: childSpan}
	second := &runtimeObject{id: 2, class: "Child", fields: map[string]runtimeValueInternal{}, allocationSpan: childSpan}
	shared := &runtimeObject{id: 3, class: "Object", fields: map[string]runtimeValueInternal{}, allocationSpan: parentSpan}
	first.fields = map[string]runtimeValueInternal{
		"inherited": intValue(7), "selfref": objectValue(first), "peer": objectValue(second),
		"shared": objectValue(shared), "text": stringValue("abcdefghij"),
	}
	second.fields = map[string]runtimeValueInternal{
		"inherited": intValue(8), "selfref": objectValue(second), "peer": objectValue(first),
		"shared": objectValue(shared), "text": stringValue("short"),
	}
	vm := &VM{
		exec:    &executable{Classes: map[string]*runtimeClass{"Child": class, "Object": {Name: "Object"}}},
		objects: map[int]*runtimeObject{3: shared, 1: first, 2: second},
		frames:  []*vmFrame{{method: &compiledMethod{Name: "main", Owner: "Main"}, self: objectValue(first), scopes: []map[string]runtimeValueInternal{{"z": objectValue(second), "a": objectValue(shared)}}}},
		stack:   []runtimeValueInternal{objectValue(first)},
	}
	query := HeapQuery{Mode: "graph", Limits: HeapLimits{MaxObjects: 10, MaxFields: 10, MaxDepth: 10, StringPreview: 4, MaxBytes: 64 << 10}}
	firstSnapshot, err := vm.heapSnapshot(query)
	if err != nil {
		t.Fatal(err)
	}
	secondSnapshot, err := vm.heapSnapshot(query)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstSnapshot, secondSnapshot) {
		t.Fatal("heap extraction changed between identical queries")
	}
	if len(firstSnapshot.Objects) != 3 || firstSnapshot.Objects[0].ID != "Child#1" || firstSnapshot.Objects[1].ID != "Child#2" || firstSnapshot.Objects[2].ID != "Object#3" {
		t.Fatalf("object order = %+v", firstSnapshot.Objects)
	}
	fields := firstSnapshot.Objects[0].Fields
	if len(fields) != 5 || fields[0].Name != "inherited" || fields[0].Owner != "Parent" || fields[0].StaticType != "Int" || fields[1].Name != "selfref" {
		t.Fatalf("field order/metadata = %+v", fields)
	}
	if fields[1].Value.ObjectID != "Child#1" || fields[2].Value.ObjectID != "Child#2" || fields[3].Value.ObjectID != "Object#3" {
		t.Fatalf("cycle/sharing references = %+v", fields)
	}
	if fields[4].Value.Display != `"abcd"` || fields[4].Value.FullLength != 10 || !fields[4].Value.Truncated {
		t.Fatalf("string preview = %+v", fields[4].Value)
	}
	labels := []string{}
	for _, root := range firstSnapshot.Roots {
		labels = append(labels, root.Label)
	}
	if strings.Join(labels, ",") != "frame[0].self,frame[0].scope[0].a,frame[0].scope[0].z,stack[0]" {
		t.Fatalf("root order = %v", labels)
	}
	encoded, err := json.Marshal(firstSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "0x") || strings.Contains(string(encoded), "uintptr") {
		t.Fatalf("heap leaked implementation address: %s", encoded)
	}
}

func TestHeapLimitsDepthContinuationAndByteBound(t *testing.T) {
	const count = 12
	objects := map[int]*runtimeObject{}
	for id := 1; id <= count; id++ {
		objects[id] = &runtimeObject{id: id, class: "Node", fields: map[string]runtimeValueInternal{"payload": stringValue(strings.Repeat("x", 300))}}
	}
	for id := 1; id < count; id++ {
		objects[id].fields["next"] = objectValue(objects[id+1])
	}
	objects[count].fields["next"] = voidValue()
	vm := &VM{objects: objects, frames: []*vmFrame{{method: &compiledMethod{}, self: objectValue(objects[1])}}}

	graph, err := vm.heapSnapshot(HeapQuery{Mode: "graph", Limits: HeapLimits{MaxObjects: 10, MaxFields: 2, MaxDepth: 2, StringPreview: 8, MaxBytes: 4 << 10}})
	if err != nil {
		t.Fatal(err)
	}
	if !graph.Truncated || len(graph.Objects) != 3 || graph.Objects[2].Fields[1].Name != "payload" || !graph.Objects[2].Fields[1].Value.Truncated {
		t.Fatalf("depth/string truncation = %+v", graph)
	}
	if graph.Objects[2].Fields[0].Name != "next" || graph.Objects[2].Fields[0].Value.Kind != "reference" || !graph.Objects[2].Fields[0].Value.Unavailable {
		t.Fatalf("omitted reference looked available or void: %+v", graph.Objects[2].Fields[0].Value)
	}

	search, err := vm.heapSnapshot(HeapQuery{Mode: "search", Offset: 2, Class: "Node", Limits: HeapLimits{MaxObjects: 3, MaxFields: 1, MaxDepth: 1, StringPreview: 8, MaxBytes: 4 << 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Objects) != 3 || search.Objects[0].ID != "Node#3" || search.NextOffset != 5 || !search.Truncated {
		t.Fatalf("search continuation = %+v", search)
	}

	bounded, err := vm.heapSnapshot(HeapQuery{Mode: "graph", Limits: HeapLimits{MaxObjects: 10, MaxFields: 2, MaxDepth: 10, StringPreview: 128, MaxBytes: 1_024}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(bounded)
	if len(encoded) > 1_024 || !bounded.Truncated {
		t.Fatalf("serialized heap bound = reported %d actual %d truncated %v", bounded.Serialized, len(encoded), bounded.Truncated)
	}
}

func TestHeapTracksInheritanceAndObjectCopy(t *testing.T) {
	const source = `class Parent { inherited : Int <- 7; };
class Box inherits Parent { own : String <- "box"; };
class Main {
  first : Box <- new Box;
  second : Box;
  main() : Object { { second <- first.copy(); self; } };
};`
	session := NewDebugSession(source)
	finished := session.Run(100_000, nil)
	if !finished.Runtime.Halted {
		t.Fatalf("copy fixture failed: %+v", finished.Runtime.Diagnostics)
	}
	heap, err := session.Heap(HeapQuery{Mode: "search", Class: "Box"})
	if err != nil {
		t.Fatal(err)
	}
	if len(heap.Objects) != 2 {
		t.Fatalf("copy objects = %+v", heap.Objects)
	}
	for _, object := range heap.Objects {
		if len(object.Fields) != 2 || object.Fields[0].Name != "inherited" || object.Fields[0].Owner != "Parent" || object.Fields[1].Name != "own" || object.Fields[1].Owner != "Box" {
			t.Fatalf("copy inherited fields = %+v", object.Fields)
		}
	}
	if !reflect.DeepEqual(heap.Objects[0].Fields, heap.Objects[1].Fields) {
		t.Fatalf("Object.copy did not preserve shallow field values: %+v", heap.Objects)
	}
}

func TestHeapQueriesDoNotMutateVM(t *testing.T) {
	session := NewDebugSession("class Main { main() : Object { self }; };")
	session.Run(12, nil)
	before := session.Snapshot(false, 0)
	for _, query := range []HeapQuery{{Mode: "summary"}, {Mode: "roots"}, {Mode: "graph"}, {Mode: "search", Class: "Main"}} {
		if _, err := session.Heap(query); err != nil {
			t.Fatal(err)
		}
	}
	after := session.Snapshot(false, 0)
	if !reflect.DeepEqual(before.Runtime, after.Runtime) || before.StopReason != after.StopReason {
		t.Fatal("read-only heap query mutated VM state")
	}
}
