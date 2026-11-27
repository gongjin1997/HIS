package configcenter

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// K8sServiceSpec defines normalized CPU (milli), Memory (MiB), and replica specifications (LiveOps 03.5 §3.1).
type K8sServiceSpec struct {
	ServiceName string `json:"serviceName"`
	ParamPrefix string `json:"paramPrefix"`
	CPUApply    string `json:"cpuApply"` // e.g. "500m" or "2"
	CPULimit    string `json:"cpuLimit"` // e.g. "2000m" or "4"
	MemApply    string `json:"memApply"` // e.g. "512Mi" or "2Gi"
	MemLimit    string `json:"memLimit"` // e.g. "1Gi" or "4Gi"
	Replicas    int    `json:"replicas"`
}

// NormalizedSpec holds integer milliCPU & MiB bounds after strict validation.
type NormalizedSpec struct {
	ServiceName   string `json:"serviceName"`
	ParamPrefix   string `json:"paramPrefix"`
	CPUApplyMilli int64  `json:"cpuApplyMilli"`
	CPULimitMilli int64  `json:"cpuLimitMilli"`
	MemApplyMi    int64  `json:"memApplyMi"`
	MemLimitMi    int64  `json:"memLimitMi"`
	Replicas      int    `json:"replicas"`
}

// ParseCPUMilli normalizes K8s CPU strings ("500m" -> 500, "2" -> 2000, "0.5" -> 500).
func ParseCPUMilli(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("CPU specification cannot be empty")
	}
	if strings.HasSuffix(s, "m") {
		v, err := strconv.ParseInt(strings.TrimSuffix(s, "m"), 10, 64)
		if err != nil || v <= 0 {
			return 0, fmt.Errorf("invalid milliCPU %q", raw)
		}
		return v, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0, fmt.Errorf("invalid CPU cores %q", raw)
	}
	return int64(math.Round(f * 1000)), nil
}

// ParseMemMiB normalizes K8s memory strings ("512Mi" -> 512, "4Gi" -> 4096, "1024M" -> 1024).
func ParseMemMiB(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("memory specification cannot be empty")
	}
	upper := strings.ToUpper(s)
	switch {
	case strings.HasSuffix(upper, "GI") || strings.HasSuffix(upper, "G"):
		num := strings.TrimRight(upper, "GI")
		f, err := strconv.ParseFloat(num, 64)
		if err != nil || f <= 0 {
			return 0, fmt.Errorf("invalid memory GiB %q", raw)
		}
		return int64(math.Round(f * 1024)), nil
	case strings.HasSuffix(upper, "MI") || strings.HasSuffix(upper, "M"):
		num := strings.TrimRight(upper, "MI")
		f, err := strconv.ParseFloat(num, 64)
		if err != nil || f <= 0 {
			return 0, fmt.Errorf("invalid memory MiB %q", raw)
		}
		return int64(math.Round(f)), nil
	default:
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v <= 0 {
			return 0, fmt.Errorf("invalid memory value %q (use Mi/Gi suffix)", raw)
		}
		return v, nil
	}
}

// NormalizeAndValidateSpec validates 0 < apply <= limit and replicas > 0, returning normalized integer units.
func NormalizeAndValidateSpec(spec K8sServiceSpec) (*NormalizedSpec, error) {
	ca, err := ParseCPUMilli(spec.CPUApply)
	if err != nil {
		return nil, fmt.Errorf("cpuApply: %w", err)
	}
	cl, err := ParseCPUMilli(spec.CPULimit)
	if err != nil {
		return nil, fmt.Errorf("cpuLimit: %w", err)
	}
	if ca > cl {
		return nil, fmt.Errorf("CPU 请求 (%dm) 不能大于上限 (%dm)", ca, cl)
	}
	ma, err := ParseMemMiB(spec.MemApply)
	if err != nil {
		return nil, fmt.Errorf("memApply: %w", err)
	}
	ml, err := ParseMemMiB(spec.MemLimit)
	if err != nil {
		return nil, fmt.Errorf("memLimit: %w", err)
	}
	if ma > ml {
		return nil, fmt.Errorf("内存请求 (%dMi) 不能大于上限 (%dMi)", ma, ml)
	}
	rep := spec.Replicas
	if rep <= 0 {
		return nil, fmt.Errorf("replicas 必须大于 0")
	}
	prefix := strings.TrimSpace(spec.ParamPrefix)
	if prefix == "" {
		prefix = strings.TrimSpace(spec.ServiceName)
	}
	return &NormalizedSpec{
		ServiceName:   spec.ServiceName,
		ParamPrefix:   prefix,
		CPUApplyMilli: ca,
		CPULimitMilli: cl,
		MemApplyMi:    ma,
		MemLimitMi:    ml,
		Replicas:      rep,
	}, nil
}

// EffectiveParamEntry traces which layer won in the strict 3-level merge (Spec > Service > Global).
type EffectiveParamEntry struct {
	Key    string `json:"key"`
	Value  any    `json:"value"`
	Source string `json:"source"` // global | service | spec
}

// ResolveEffectiveParams implements the single deterministic 3-level merge function (LiveOps 03.5 §3.1):
// Layer 1 (lowest): globalParams
// Layer 2 (middle): serviceParams
// Layer 3 (highest): spec-derived parameters (spec@{prefix}CPU / CPULimit / Memory / MemoryLimit / Replicas)
func ResolveEffectiveParams(globalParams, serviceParams map[string]any, specs []K8sServiceSpec) (map[string]any, []EffectiveParamEntry, []NormalizedSpec, error) {
	merged := make(map[string]any)
	sources := make(map[string]string)

	for k, v := range globalParams {
		merged[k] = v
		sources[k] = "global"
	}
	for k, v := range serviceParams {
		merged[k] = v
		sources[k] = "service"
	}

	normSpecs := make([]NormalizedSpec, 0, len(specs))
	for _, sp := range specs {
		ns, err := NormalizeAndValidateSpec(sp)
		if err != nil {
			return nil, nil, nil, err
		}
		normSpecs = append(normSpecs, *ns)
		derived := map[string]any{
			fmt.Sprintf("spec@%sCPU", ns.ParamPrefix):         fmt.Sprintf("%dm", ns.CPUApplyMilli),
			fmt.Sprintf("spec@%sCPULimit", ns.ParamPrefix):    fmt.Sprintf("%dm", ns.CPULimitMilli),
			fmt.Sprintf("spec@%sMemory", ns.ParamPrefix):      fmt.Sprintf("%dMi", ns.MemApplyMi),
			fmt.Sprintf("spec@%sMemoryLimit", ns.ParamPrefix): fmt.Sprintf("%dMi", ns.MemLimitMi),
			fmt.Sprintf("spec@%sReplicas", ns.ParamPrefix):    ns.Replicas,
		}
		for k, v := range derived {
			merged[k] = v
			sources[k] = "spec"
		}
	}

	trace := make([]EffectiveParamEntry, 0, len(merged))
	for k, v := range merged {
		trace = append(trace, EffectiveParamEntry{Key: k, Value: v, Source: sources[k]})
	}
	return merged, trace, normSpecs, nil
}
