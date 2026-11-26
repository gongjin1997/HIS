package osadapter

import (
	"fmt"
	"net/http"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// PkgMapSpec mirrors spec/pkgmap.yaml (64 logical packages + 6 conflict entries).
type PkgMapSpec struct {
	APIVersion string          `yaml:"apiVersion"`
	Kind       string          `yaml:"kind"`
	Metadata   PkgMapMetadata  `yaml:"metadata"`
	Packages   []LogicalPkg    `yaml:"packages"`
	Conflicts  []ConflictEntry `yaml:"conflicts"`
}

type PkgMapMetadata struct {
	Revision string `yaml:"revision"`
	DataAsOf string `yaml:"dataAsOf"`
}

type LogicalPkg struct {
	Name      string            `yaml:"name"`
	Class     string            `yaml:"class"` // required | conditional | optional | detect
	When      string            `yaml:"when"`
	Purpose   string            `yaml:"purpose"`
	Provides  []string          `yaml:"provides"`
	Default   map[string]string `yaml:"default"` // rpm / deb (null unmarshals to "")
	Overrides []PkgOverride     `yaml:"overrides"`
}

type PkgOverride struct {
	When PkgOverrideWhen `yaml:"when"`
	Pkg  *string         `yaml:"pkg"` // nil means explicitly unavailable (pkg: null)
}

type PkgOverrideWhen struct {
	Family string   `yaml:"family"`
	Rule   []string `yaml:"rule"`
}

type ConflictEntry struct {
	Name       string              `yaml:"name"`
	Provides   []string            `yaml:"provides"`
	Candidates map[string][]string `yaml:"candidates"`
}

// ResolvePkg resolves a logical package name for the matched rule following the exact priority:
// 1. rule override (when.rule)
// 2. family chain override (when.family, closest first)
// 3. pkgType default (rpm / deb)
// Returns (pkgName, missing, error). missing=true corresponds to pkg: null.
func (e *Engine) ResolvePkg(logical string, rule *RuleSpec) (string, bool, error) {
	if rule == nil {
		return "", true, sdk.NewError(sdk.CodeOSUnsupported, "OS", http.StatusUnprocessableEntity, sdk.CodeOSUnsupported, false,
			fmt.Errorf("nil os rule when resolving package %q", logical))
	}
	var lp *LogicalPkg
	for i := range e.PkgMap.Packages {
		if e.PkgMap.Packages[i].Name == logical {
			lp = &e.PkgMap.Packages[i]
			break
		}
	}
	if lp == nil {
		return "", true, sdk.NewError(sdk.CodeOSPkgMissing, "OS", http.StatusUnprocessableEntity, sdk.CodeOSPkgMissing, false,
			fmt.Errorf("unknown logical package %q", logical))
	}
	// 1. Rule override
	for _, ov := range lp.Overrides {
		for _, rid := range ov.When.Rule {
			if rid == rule.ID {
				if ov.Pkg == nil || *ov.Pkg == "" {
					return "", true, nil
				}
				return *ov.Pkg, false, nil
			}
		}
	}
	// 2. Family chain override
	for _, fam := range e.FamilyChain(rule) {
		for _, ov := range lp.Overrides {
			if ov.When.Family != "" && ov.When.Family == fam {
				if ov.Pkg == nil || *ov.Pkg == "" {
					return "", true, nil
				}
				return *ov.Pkg, false, nil
			}
		}
	}
	// 3. pkgType default
	pt := e.RuleAttr(rule, "pkgType")
	def := lp.Default[pt]
	if def == "" {
		return "", true, nil
	}
	return def, false, nil
}
