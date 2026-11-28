package dag

import (
	"fmt"
	"sort"
	"strings"
)

// Node defines a schedulable unit and its upstream prerequisites.
type Node struct {
	ID        string
	DependsOn []string
}

// Plan holds the deterministic topological execution order and parallelizable wave layers.
type Plan struct {
	Order  []string   `json:"order"`
	Layers [][]string `json:"layers"`
}

// CycleError reports the exact nodes involved in a dependency cycle.
type CycleError struct {
	Nodes []string
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("cyclic dependency detected among nodes: %s", strings.Join(e.Nodes, ", "))
}

// Sort executes deterministic Kahn topological sorting with wave-layer grouping.
func Sort(nodes []Node) (*Plan, error) {
	if len(nodes) == 0 {
		return &Plan{Order: make([]string, 0), Layers: make([][]string, 0)}, nil
	}
	inDegree := make(map[string]int, len(nodes))
	dependents := make(map[string][]string, len(nodes))
	exists := make(map[string]struct{}, len(nodes))

	for _, n := range nodes {
		if n.ID == "" {
			return nil, fmt.Errorf("dag node has empty ID")
		}
		if _, dup := exists[n.ID]; dup {
			return nil, fmt.Errorf("duplicate dag node %q", n.ID)
		}
		exists[n.ID] = struct{}{}
		inDegree[n.ID] = 0
	}

	for _, n := range nodes {
		seenDep := make(map[string]struct{}, len(n.DependsOn))
		for _, dep := range n.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			if dep == n.ID {
				return nil, &CycleError{Nodes: []string{n.ID}}
			}
			if _, ok := exists[dep]; !ok {
				return nil, fmt.Errorf("node %q depends on unknown node %q", n.ID, dep)
			}
			if _, dup := seenDep[dep]; dup {
				continue
			}
			seenDep[dep] = struct{}{}
			dependents[dep] = append(dependents[dep], n.ID)
			inDegree[n.ID]++
		}
	}

	ready := make([]string, 0, len(nodes))
	for id, deg := range inDegree {
		if deg == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	order := make([]string, 0, len(nodes))
	layers := make([][]string, 0)

	for len(ready) > 0 {
		wave := make([]string, len(ready))
		copy(wave, ready)
		layers = append(layers, wave)
		nextReady := make([]string, 0)

		for _, cur := range wave {
			order = append(order, cur)
			for _, nxt := range dependents[cur] {
				inDegree[nxt]--
				if inDegree[nxt] == 0 {
					nextReady = append(nextReady, nxt)
				}
			}
		}
		sort.Strings(nextReady)
		ready = nextReady
	}

	if len(order) != len(nodes) {
		stuck := make([]string, 0)
		for id, deg := range inDegree {
			if deg > 0 {
				stuck = append(stuck, id)
			}
		}
		sort.Strings(stuck)
		return nil, &CycleError{Nodes: stuck}
	}

	return &Plan{Order: order, Layers: layers}, nil
}
