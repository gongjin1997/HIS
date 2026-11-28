package dag

import (
	"fmt"
	"sort"
	"strings"
)

const (
	MaxNodes = 2000
	MaxEdges = 8000
)

type NodeSpec struct {
	Key       string `json:"key"`
	TaskOrder int    `json:"taskOrder"`
	Disabled  bool   `json:"disabled,omitempty"`
}

type EdgeSpec struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type CompiledPlan struct {
	Order        []string       `json:"order"`
	Layers       [][]string     `json:"layers"`
	ActiveNodes  []NodeSpec     `json:"activeNodes"`
	ActiveEdges  []EdgeSpec     `json:"activeEdges"`
	InitialReady []string       `json:"initialReady"`
	InDegree     map[string]int `json:"inDegree"`
}

type CycleError struct {
	BlockedNodes []string `json:"blockedNodes"`
	CyclePath    []string `json:"cyclePath"`
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("cyclic dependency detected: %s (blocked: %s)",
		strings.Join(e.CyclePath, " -> "), strings.Join(e.BlockedNodes, ", "))
}

// Compile validates an explicit DAG (unique keys, missing endpoints, self-loops, duplicate edges,
// disabled node edge prohibition, 2000-node / 8000-edge cap) and runs deterministic Kahn topological sort.
// If explicit edges are empty, enabled nodes are chained sequentially in taskOrder (preserving original taskOrder).
func Compile(nodes []NodeSpec, edges []EdgeSpec, explicitGraph bool) (*CompiledPlan, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("计划至少需要包含 1 个启用节点")
	}

	nodeMap := make(map[string]NodeSpec, len(nodes))
	activeNodes := make([]NodeSpec, 0, len(nodes))
	for _, n := range nodes {
		k := strings.TrimSpace(n.Key)
		if k == "" {
			return nil, fmt.Errorf("存在空的节点标识")
		}
		if _, dup := nodeMap[k]; dup {
			return nil, fmt.Errorf("节点标识重复: %q", k)
		}
		n.Key = k
		nodeMap[k] = n
		if !n.Disabled {
			activeNodes = append(activeNodes, n)
		}
	}
	if len(activeNodes) == 0 {
		return nil, fmt.Errorf("没有处于启用状态的活动节点")
	}
	if len(activeNodes) > MaxNodes {
		return nil, fmt.Errorf("启用节点数 %d 超出上限 %d", len(activeNodes), MaxNodes)
	}

	resolvedEdges := make([]EdgeSpec, 0, len(edges))
	if !explicitGraph && len(edges) == 0 {
		// Legacy sequential chain: skip disabled nodes while preserving original taskOrder (LiveOps 01-提交部署作业 §2).
		sorted := append([]NodeSpec(nil), activeNodes...)
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].TaskOrder != sorted[j].TaskOrder {
				return sorted[i].TaskOrder < sorted[j].TaskOrder
			}
			return sorted[i].Key < sorted[j].Key
		})
		for i := 0; i < len(sorted)-1; i++ {
			resolvedEdges = append(resolvedEdges, EdgeSpec{From: sorted[i].Key, To: sorted[i+1].Key})
		}
	} else {
		seenEdge := make(map[string]struct{}, len(edges))
		for _, e := range edges {
			from, to := strings.TrimSpace(e.From), strings.TrimSpace(e.To)
			if from == "" || to == "" {
				return nil, fmt.Errorf("依赖边起终点不能为空")
			}
			if from == to {
				return nil, &CycleError{BlockedNodes: []string{from}, CyclePath: []string{from, from}}
			}
			srcN, ok1 := nodeMap[from]
			dstN, ok2 := nodeMap[to]
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("依赖边 %s -> %s 引用了不存在的节点", from, to)
			}
			if srcN.Disabled || dstN.Disabled {
				return nil, fmt.Errorf("显式依赖边 %s -> %s 禁止引用已停用节点", from, to)
			}
			ek := from + "\x00" + to
			if _, dup := seenEdge[ek]; dup {
				return nil, fmt.Errorf("存在重复依赖边 %s -> %s", from, to)
			}
			seenEdge[ek] = struct{}{}
			resolvedEdges = append(resolvedEdges, EdgeSpec{From: from, To: to})
		}
	}

	if len(resolvedEdges) > MaxEdges {
		return nil, fmt.Errorf("依赖边数 %d 超出上限 %d", len(resolvedEdges), MaxEdges)
	}

	inDeg := make(map[string]int, len(activeNodes))
	adj := make(map[string][]string, len(activeNodes))
	for _, n := range activeNodes {
		inDeg[n.Key] = 0
	}
	for _, e := range resolvedEdges {
		adj[e.From] = append(adj[e.From], e.To)
		inDeg[e.To]++
	}

	origInDeg := make(map[string]int, len(inDeg))
	ready := make([]string, 0)
	for k, d := range inDeg {
		origInDeg[k] = d
		if d == 0 {
			ready = append(ready, k)
		}
	}
	sort.Strings(ready)
	initialReady := append([]string(nil), ready...)

	order := make([]string, 0, len(activeNodes))
	layers := make([][]string, 0)
	for len(ready) > 0 {
		wave := append([]string(nil), ready...)
		layers = append(layers, wave)
		nextReady := make([]string, 0)
		for _, u := range wave {
			order = append(order, u)
			for _, v := range adj[u] {
				inDeg[v]--
				if inDeg[v] == 0 {
					nextReady = append(nextReady, v)
				}
			}
		}
		sort.Strings(nextReady)
		ready = nextReady
	}

	if len(order) != len(activeNodes) {
		blocked := make([]string, 0)
		blockedSet := make(map[string]bool)
		for k, d := range inDeg {
			if d > 0 {
				blocked = append(blocked, k)
				blockedSet[k] = true
			}
		}
		sort.Strings(blocked)
		return nil, &CycleError{
			BlockedNodes: blocked,
			CyclePath:    findConcreteCycle(blocked, blockedSet, adj),
		}
	}

	return &CompiledPlan{
		Order:        order,
		Layers:       layers,
		ActiveNodes:  activeNodes,
		ActiveEdges:  resolvedEdges,
		InitialReady: initialReady,
		InDegree:     origInDeg,
	}, nil
}

func findConcreteCycle(blocked []string, blockedSet map[string]bool, adj map[string][]string) []string {
	state := make(map[string]int, len(blocked)) // 0=unvisited, 1=visiting, 2=done
	stack := make([]string, 0, len(blocked))
	pos := make(map[string]int, len(blocked))
	var cycle []string

	var dfs func(u string) bool
	dfs = func(u string) bool {
		state[u] = 1
		pos[u] = len(stack)
		stack = append(stack, u)
		nxtList := append([]string(nil), adj[u]...)
		sort.Strings(nxtList)
		for _, v := range nxtList {
			if !blockedSet[v] {
				continue
			}
			if state[v] == 1 {
				cycle = append([]string(nil), stack[pos[v]:]...)
				cycle = append(cycle, v)
				return true
			}
			if state[v] == 0 && dfs(v) {
				return true
			}
		}
		stack = stack[:len(stack)-1]
		state[u] = 2
		return false
	}

	for _, start := range blocked {
		if state[start] == 0 && dfs(start) {
			return cycle
		}
	}
	return blocked
}
