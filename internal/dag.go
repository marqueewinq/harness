package internal

import (
	"fmt"
	"sort"
)

// TopoSort returns check names in topological order.
func TopoSort(checks map[string]CheckConfig) ([]string, error) {
	for name, check := range checks {
		for _, dep := range check.Needs {
			if _, ok := checks[dep]; !ok {
				return nil, fmt.Errorf("check %q depends on unknown check %q", name, dep)
			}
		}
	}

	inDegree := map[string]int{}
	dependents := map[string][]string{}

	for name := range checks {
		inDegree[name] = 0
	}
	for name, check := range checks {
		inDegree[name] = len(check.Needs)
		for _, dep := range check.Needs {
			dependents[dep] = append(dependents[dep], name)
		}
	}

	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue)

	var result []string
	for len(queue) > 0 {
		sort.Strings(queue)
		node := queue[0]
		queue = queue[1:]
		result = append(result, node)

		for _, dep := range dependents[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(result) != len(checks) {
		return nil, fmt.Errorf("cycle detected in check dependencies")
	}

	return result, nil
}

// ReadyChecks returns check names whose dependencies are all in the completed set.
func ReadyChecks(checks map[string]CheckConfig, completed map[string]bool) []string {
	var ready []string
	for name, check := range checks {
		if completed[name] {
			continue
		}
		allMet := true
		for _, dep := range check.Needs {
			if !completed[dep] {
				allMet = false
				break
			}
		}
		if allMet {
			ready = append(ready, name)
		}
	}
	sort.Strings(ready)
	return ready
}
