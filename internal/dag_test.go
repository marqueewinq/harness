package internal

import (
	"reflect"
	"testing"
)

func TestTopoSort(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		"install": {Run: "npm install"},
		"lint":    {Run: "npm run lint", Needs: []string{"install"}},
		"test":    {Run: "make test", Needs: []string{"install"}},
		"build":   {Run: "make build", Needs: []string{"test", "lint"}},
	}

	order, err := TopoSort(checks)
	if err != nil {
		t.Fatal(err)
	}

	pos := map[string]int{}
	for i, name := range order {
		pos[name] = i
	}

	if pos["install"] >= pos["lint"] {
		t.Errorf("install(%d) should come before lint(%d)", pos["install"], pos["lint"])
	}
	if pos["install"] >= pos["test"] {
		t.Errorf("install(%d) should come before test(%d)", pos["install"], pos["test"])
	}
	if pos["lint"] >= pos["build"] {
		t.Errorf("lint(%d) should come before build(%d)", pos["lint"], pos["build"])
	}
	if pos["test"] >= pos["build"] {
		t.Errorf("test(%d) should come before build(%d)", pos["test"], pos["build"])
	}
}

func TestTopoSortCycle(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		"a": {Run: "a", Needs: []string{"b"}},
		"b": {Run: "b", Needs: []string{"a"}},
	}
	_, err := TopoSort(checks)
	if err == nil {
		t.Error("expected cycle error")
	}
}

func TestTopoSortUnknownDep(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		"test": {Run: "test", Needs: []string{"nonexistent"}},
	}
	_, err := TopoSort(checks)
	if err == nil {
		t.Error("expected unknown dependency error")
	}
}

func TestTopoSortSingle(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		"test": {Run: "make test"},
	}
	order, err := TopoSort(checks)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"test"}) {
		t.Errorf("order = %v, want [test]", order)
	}
}

func TestReadyChecks(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		"install": {Run: "npm install"},
		"lint":    {Run: "lint", Needs: []string{"install"}},
		"test":    {Run: "test", Needs: []string{"install"}},
		"build":   {Run: "build", Needs: []string{"test", "lint"}},
	}

	ready := ReadyChecks(checks, map[string]bool{})
	if !reflect.DeepEqual(ready, []string{"install"}) {
		t.Errorf("ready = %v, want [install]", ready)
	}

	ready = ReadyChecks(checks, map[string]bool{"install": true})
	if len(ready) != 2 {
		t.Errorf("ready = %v, want [lint, test]", ready)
	}

	ready = ReadyChecks(checks, map[string]bool{"install": true, "lint": true, "test": true})
	if !reflect.DeepEqual(ready, []string{"build"}) {
		t.Errorf("ready = %v, want [build]", ready)
	}
}

func TestTopoSortEmpty(t *testing.T) {
	t.Parallel()
	order, err := TopoSort(map[string]CheckConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 0 {
		t.Errorf("TopoSort(empty) = %v, want empty", order)
	}
}

func TestTopoSortSelfRef(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		"a": {Run: "a", Needs: []string{"a"}},
	}
	_, err := TopoSort(checks)
	if err == nil {
		t.Error("expected error for self-referencing dependency")
	}
}

func TestTopoSortDeepChain(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		"a": {Run: "a"},
		"b": {Run: "b", Needs: []string{"a"}},
		"c": {Run: "c", Needs: []string{"b"}},
		"d": {Run: "d", Needs: []string{"c"}},
	}
	order, err := TopoSort(checks)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"a", "b", "c", "d"}) {
		t.Errorf("order = %v, want [a b c d]", order)
	}
}

func TestReadyChecksAllDone(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		"a": {Run: "a"},
		"b": {Run: "b", Needs: []string{"a"}},
	}
	completed := map[string]bool{"a": true, "b": true}
	ready := ReadyChecks(checks, completed)
	if len(ready) != 0 {
		t.Errorf("ReadyChecks (all done) = %v, want empty", ready)
	}
}

func TestReadyChecksEmpty(t *testing.T) {
	t.Parallel()
	ready := ReadyChecks(map[string]CheckConfig{}, map[string]bool{})
	if len(ready) != 0 {
		t.Errorf("ReadyChecks (empty) = %v, want empty", ready)
	}
}
