package osadapter

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
	"github.com/kinstall/kinstall/spec"
)

// OSMatchSpec mirrors spec/osmatch.yaml.
type OSMatchSpec struct {
	APIVersion string                `yaml:"apiVersion"`
	Kind       string                `yaml:"kind"`
	Metadata   OSMatchMetadata       `yaml:"metadata"`
	Floor      OSMatchFloor          `yaml:"floor"`
	Families   map[string]FamilySpec `yaml:"families"`
	Rules      []RuleSpec            `yaml:"rules"`
}

type OSMatchMetadata struct {
	Revision    string `yaml:"revision"`
	DataAsOf    string `yaml:"dataAsOf"`
	Evidence    string `yaml:"evidence"`
	EOLSoonDays int    `yaml:"eolSoonDays"`
}

type OSMatchFloor struct {
	Arch      []string `yaml:"arch"`
	KernelMin string   `yaml:"kernelMin"`
	Init      string   `yaml:"init"`
}

type FamilySpec struct {
	Parent     string   `yaml:"parent"`
	PkgType    string   `yaml:"pkgType"`
	PkgManager string   `yaml:"pkgManager"`
	Lock       string   `yaml:"lock"`
	Firewall   string   `yaml:"firewall"`
	MAC        string   `yaml:"mac"`
	Network    string   `yaml:"network"`
	Bootloader string   `yaml:"bootloader"`
	TimeSync   string   `yaml:"timeSync"`
	Security   []string `yaml:"security"`
}

type RuleSpec struct {
	ID        string                 `yaml:"id"`
	Display   string                 `yaml:"display"`
	Family    string                 `yaml:"family"`
	Parent    string                 `yaml:"parent"`
	Priority  int                    `yaml:"priority"`
	Match     map[string]any         `yaml:"match"`
	Variants  map[string]VariantSpec `yaml:"variants"`
	Overrides map[string]any         `yaml:"overrides"`
	Support   RuleSupport            `yaml:"support"`
	Quirks    []string               `yaml:"quirks"`
}

type VariantSpec struct {
	From    string            `yaml:"from"`
	Key     string            `yaml:"key"`
	Path    string            `yaml:"path"`
	Pattern string            `yaml:"pattern"`
	Default string            `yaml:"default"`
	Map     map[string]string `yaml:"map"`
}

type RuleSupport struct {
	Tier      string   `yaml:"tier"`
	Evidence  string   `yaml:"evidence"`
	Samples   []string `yaml:"samples"`
	PkgSource string   `yaml:"pkgSource"`
}

// MatchInput provides parsed /etc/os-release key/values and auxiliary file contents (e.g. /etc/.kyinfo).
type MatchInput struct {
	OSRelease map[string]string
	Files     map[string]string
}

// MatchResult carries the matched rule, resolved family chain, and extracted variants.
type MatchResult struct {
	Rule        *RuleSpec
	FamilyChain []string
	Variants    map[string]string
}

// Engine holds loaded osmatch.yaml and pkgmap.yaml specifications.
type Engine struct {
	OSMatch *OSMatchSpec
	PkgMap  *PkgMapSpec
}

// LoadEmbedded loads the compiled-in spec/osmatch.yaml and spec/pkgmap.yaml via //go:embed.
func LoadEmbedded() (*Engine, error) {
	return LoadFromFS(spec.FS)
}

// LoadWithOverride loads embedded spec and overlays a higher-revision spec from overrideDir if present (D1 §4.2).
func LoadWithOverride(overrideDir string) (*Engine, error) {
	eng, err := LoadEmbedded()
	if err != nil {
		return nil, err
	}
	if overrideDir == "" {
		return eng, nil
	}
	omPath := filepath.Join(overrideDir, "osmatch.yaml")
	pmPath := filepath.Join(overrideDir, "pkgmap.yaml")
	if _, statErr := os.Stat(omPath); statErr != nil {
		return eng, nil //nolint:nilerr // optional override directory absent; keep embedded spec
	}
	omRaw, err := os.ReadFile(omPath)
	if err != nil {
		return nil, err
	}
	var om OSMatchSpec
	if err := yaml.Unmarshal(omRaw, &om); err != nil {
		return nil, err
	}
	if om.Metadata.Revision > eng.OSMatch.Metadata.Revision {
		eng.OSMatch = &om
		if pmRaw, perr := os.ReadFile(pmPath); perr == nil {
			var pm PkgMapSpec
			if uerr := yaml.Unmarshal(pmRaw, &pm); uerr == nil {
				eng.PkgMap = &pm
			}
		}
	}
	return eng, nil
}

// LoadFromFS parses osmatch.yaml and pkgmap.yaml from fsys.
func LoadFromFS(fsys fs.FS) (*Engine, error) {
	omBytes, err := fs.ReadFile(fsys, "osmatch.yaml")
	if err != nil {
		return nil, fmt.Errorf("read osmatch.yaml: %w", err)
	}
	var om OSMatchSpec
	if err := yaml.Unmarshal(omBytes, &om); err != nil {
		return nil, fmt.Errorf("parse osmatch.yaml: %w", err)
	}
	pmBytes, err := fs.ReadFile(fsys, "pkgmap.yaml")
	if err != nil {
		return nil, fmt.Errorf("read pkgmap.yaml: %w", err)
	}
	var pm PkgMapSpec
	if err := yaml.Unmarshal(pmBytes, &pm); err != nil {
		return nil, fmt.Errorf("parse pkgmap.yaml: %w", err)
	}
	return &Engine{OSMatch: &om, PkgMap: &pm}, nil
}

// ParseOSRelease parses standard /etc/os-release key=value text.
func ParseOSRelease(text string) map[string]string {
	out := make(map[string]string)
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}

var osFieldMap = map[string]string{
	"id":              "ID",
	"versionId":       "VERSION_ID",
	"version":         "VERSION",
	"name":            "NAME",
	"prettyName":      "PRETTY_NAME",
	"platformId":      "PLATFORM_ID",
	"versionCodename": "VERSION_CODENAME",
}

// Match evaluates all osmatch rules and returns the highest-priority unique match (ADR-024).
func (e *Engine) Match(in MatchInput) (*MatchResult, error) {
	if in.OSRelease == nil {
		in.OSRelease = map[string]string{}
	}
	if in.Files == nil {
		in.Files = map[string]string{}
	}
	var hits []*RuleSpec
	for i := range e.OSMatch.Rules {
		r := &e.OSMatch.Rules[i]
		ok, err := evalCond(r.Match, in)
		if err != nil {
			return nil, err
		}
		if ok {
			hits = append(hits, r)
		}
	}
	if len(hits) == 0 {
		return nil, nil
	}
	top := hits[0].Priority
	for _, h := range hits[1:] {
		if h.Priority > top {
			top = h.Priority
		}
	}
	var best []*RuleSpec
	for _, h := range hits {
		if h.Priority == top {
			best = append(best, h)
		}
	}
	if len(best) > 1 {
		ids := make([]string, len(best))
		for i, b := range best {
			ids[i] = b.ID
		}
		return nil, sdk.NewError(sdk.CodeOSUnsupported, "OS", http.StatusUnprocessableEntity, sdk.CodeOSUnsupported, false,
			fmt.Errorf("ambiguous osmatch rules at priority %d: %s", top, strings.Join(ids, ", ")))
	}
	winner := best[0]
	return &MatchResult{
		Rule:        winner,
		FamilyChain: e.FamilyChain(winner),
		Variants:    extractVariants(winner, in),
	}, nil
}

// FamilyChain returns the family inheritance chain from closest to root (e.g. kylin -> openeuler -> rhel-like).
func (e *Engine) FamilyChain(rule *RuleSpec) []string {
	if rule == nil || rule.Family == "detect" {
		return nil
	}
	var out []string
	f := rule.Family
	if fam, ok := e.OSMatch.Families[f]; ok && fam.Parent == "per-rule" {
		out = append(out, f)
		f = rule.Parent
	}
	for f != "" {
		out = append(out, f)
		fam, ok := e.OSMatch.Families[f]
		if !ok {
			break
		}
		f = fam.Parent
	}
	return out
}

// RuleAttr resolves a rule attribute via rule.overrides > family chain inheritance.
func (e *Engine) RuleAttr(rule *RuleSpec, key string) string {
	if rule == nil {
		return ""
	}
	if v, ok := rule.Overrides[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	for _, famName := range e.FamilyChain(rule) {
		fam, ok := e.OSMatch.Families[famName]
		if !ok {
			continue
		}
		var val string
		switch key {
		case "pkgType":
			val = fam.PkgType
		case "pkgManager":
			val = fam.PkgManager
		case "lock":
			val = fam.Lock
		case "firewall":
			val = fam.Firewall
		case "mac":
			val = fam.MAC
		case "network":
			val = fam.Network
		case "bootloader":
			val = fam.Bootloader
		case "timeSync":
			val = fam.TimeSync
		}
		if val != "" {
			return val
		}
	}
	return ""
}

func vmatch(pat any, val string, exists bool) (bool, error) {
	if !exists {
		return false, nil
	}
	var pats []any
	switch x := pat.(type) {
	case []any:
		pats = x
	default:
		pats = []any{x}
	}
	for _, p := range pats {
		ps := fmt.Sprint(p)
		if strings.HasPrefix(ps, "re:") {
			re, err := regexp.Compile(ps[3:])
			if err != nil {
				return false, err
			}
			if re.MatchString(val) {
				return true, nil
			}
		} else if strings.EqualFold(ps, val) {
			return true, nil
		}
	}
	return false, nil
}

func evalCond(m map[string]any, in MatchInput) (bool, error) {
	for k, v := range m {
		if osKey, ok := osFieldMap[k]; ok {
			val, exists := in.OSRelease[osKey]
			hit, err := vmatch(v, val, exists)
			if err != nil || !hit {
				return false, err
			}
			continue
		}
		switch k {
		case "idLike":
			tokens := strings.Fields(in.OSRelease["ID_LIKE"])
			matched := false
			for _, tok := range tokens {
				hit, err := vmatch(v, tok, true)
				if err != nil {
					return false, err
				}
				if hit {
					matched = true
					break
				}
			}
			if !matched {
				return false, nil
			}
		case "osrelease":
			sub, ok := v.(map[string]any)
			if !ok {
				return false, fmt.Errorf("osrelease condition must be map")
			}
			for kk, vv := range sub {
				val, exists := in.OSRelease[kk]
				hit, err := vmatch(vv, val, exists)
				if err != nil || !hit {
					return false, err
				}
			}
		case "file":
			fm, ok := v.(map[string]any)
			if !ok {
				return false, fmt.Errorf("file condition must be map")
			}
			p := fmt.Sprint(fm["path"])
			content, exists := in.Files[p]
			if exVal, hasEx := fm["exists"]; hasEx {
				wantEx, _ := exVal.(bool)
				if wantEx && !exists {
					return false, nil
				}
				if !wantEx && exists {
					return false, nil
				}
			}
			if containsVal, hasContains := fm["contains"]; hasContains {
				if !exists {
					return false, nil
				}
				cs := fmt.Sprint(containsVal)
				pattern := regexp.QuoteMeta(cs)
				if strings.HasPrefix(cs, "re:") {
					pattern = cs[3:]
				}
				re, err := regexp.Compile("(?m)" + pattern)
				if err != nil {
					return false, err
				}
				if !re.MatchString(content) {
					return false, nil
				}
			}
		case "anyOf":
			items, ok := v.([]any)
			if !ok {
				return false, fmt.Errorf("anyOf must be list")
			}
			anyHit := false
			for _, it := range items {
				sub, ok := it.(map[string]any)
				if !ok {
					continue
				}
				hit, err := evalCond(sub, in)
				if err != nil {
					return false, err
				}
				if hit {
					anyHit = true
					break
				}
			}
			if !anyHit {
				return false, nil
			}
		case "not":
			sub, ok := v.(map[string]any)
			if !ok {
				return false, fmt.Errorf("not must be map")
			}
			hit, err := evalCond(sub, in)
			if err != nil {
				return false, err
			}
			if hit {
				return false, nil
			}
		default:
			return false, fmt.Errorf("unknown osmatch key %q", k)
		}
	}
	return true, nil
}

func extractVariants(rule *RuleSpec, in MatchInput) map[string]string {
	out := make(map[string]string)
	for name, sp := range rule.Variants {
		var val string
		var hasVal bool
		if sp.From == "osrelease" {
			val, hasVal = in.OSRelease[sp.Key]
		} else {
			var text string
			if sp.From == "file" {
				text = in.Files[sp.Path]
			} else if osKey, ok := osFieldMap[sp.From]; ok {
				text = in.OSRelease[osKey]
			}
			if sp.Pattern != "" {
				re, err := regexp.Compile("(?m)" + sp.Pattern)
				if err == nil {
					m := re.FindStringSubmatch(text)
					idx := re.SubexpIndex("v")
					if m != nil && idx >= 0 && idx < len(m) {
						val = m[idx]
						hasVal = true
					}
				}
			} else if text != "" {
				val = text
				hasVal = true
			}
		}
		if !hasVal && sp.Default != "" {
			val = sp.Default
			hasVal = true
		}
		if hasVal {
			if mapped, ok := sp.Map[strings.ToLower(val)]; ok {
				val = mapped
			}
			out[name] = val
		}
	}
	return out
}
