// SPDX-License-Identifier: AGPL-3.0-or-later

package wasmbridge

import (
	"context"
	"fmt"

	api "github.com/ThalesMMS/cooler-compiler/compiler"
)

type Fixture struct {
	Name     string    `json:"name"`
	Requests []Request `json:"requests"`
	Expected []*State  `json:"expected"`
}

func NativeFixtures(ctx context.Context) ([]Fixture, error) {
	fixtures := []Fixture{}
	for _, example := range []struct{ name, source string }{
		{"int32", `class Main inherits IO {main():Object {out_int(2147483647+1)};};`},
		{"dynamic dispatch", `class A {f():Int {1};}; class B inherits A {f():Int {2};}; class Main inherits IO {main():Object {let x:A <- new B in out_int(x.f())};};`},
		{"static dispatch", `class A {f():Int {1};}; class B inherits A {f():Int {2};}; class Main inherits IO {main():Object {out_int((new B)@A.f())};};`},
		{"SELF_TYPE", `class Main inherits IO {copySelf():SELF_TYPE {new SELF_TYPE};main():Object {out_string(copySelf().type_name())};};`},
		{"runtime error", `class Main {main():Object {1/0};};`},
		{"semantic error", `class Main {main():Object {missing};};`},
		{"instruction bound", `class Main {main():Object {while true loop 0 pool};};`},
		{"interactive input", `class Main inherits IO {main():Object {out_string(in_string())};};`},
	} {
		fixture := Fixture{Name: example.name, Requests: []Request{{Action: "compile", Sources: []api.SourceUnit{{ID: "main", Name: "main.cl", Content: example.source}}}}}
		if example.name != "semantic error" {
			fixture.Requests = append(fixture.Requests, Request{Action: "run", Steps: 100})
		}
		if example.name == "interactive input" {
			fixture.Requests = append(fixture.Requests, Request{Action: "input", Line: "hello", Sequence: 1}, Request{Action: "run", Steps: 100})
		}
		fixtures = append(fixtures, fixture)
	}
	fixtures = append(fixtures, Fixture{Name: "multifile", Requests: []Request{
		{Action: "compile", Sources: []api.SourceUnit{{ID: "helper", Name: "helper.cl", Content: `class Helper {value():Int {42};};`}, {ID: "main", Name: "main.cl", Content: `class Main inherits IO {main():Object {out_int((new Helper).value())};};`}}},
		{Action: "run", Steps: 100},
	}})
	for i := range fixtures {
		bridge := &Bridge{}
		for j := range fixtures[i].Requests {
			fixtures[i].Requests[j].ID = j + 1
			response := bridge.Handle(ctx, fixtures[i].Requests[j])
			if response.Error != "" {
				_ = bridge.Close()
				return nil, fmt.Errorf("fixture %s: %s", fixtures[i].Name, response.Error)
			}
			fixtures[i].Expected = append(fixtures[i].Expected, response.State)
		}
		if err := bridge.Close(); err != nil {
			return nil, err
		}
	}
	return fixtures, nil
}
