package template

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"text/template"

	"github.com/Masterminds/semver/v3"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"sigs.k8s.io/yaml"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// SecretLookup resolves a named secret during template rendering.
type SecretLookup func(ctx context.Context, name string) (string, error)

// Engine renders deterministic text/template templates with missingkey=error,
// a strict self-maintained whitelist FuncMap (ADR-036, no env/rand/now),
// and a sandboxed expr v1.18.1 evaluator.
type Engine struct {
	secretLookup SecretLookup
	exprMu       sync.RWMutex
	exprCache    map[string]*vm.Program
}

// New constructs a deterministic template & expression Engine.
func New(secretLookup SecretLookup) *Engine {
	return &Engine{
		secretLookup: secretLookup,
		exprCache:    make(map[string]*vm.Program),
	}
}

// Render renders tpl with vars and registers any accessed or urlEncode'd secrets into registerSecret (D2 §4.2).
func (e *Engine) Render(ctx context.Context, tpl sdk.TemplateRef, vars map[string]any, registerSecret func(string)) ([]byte, error) {
	var raw string
	switch {
	case len(tpl.Content) > 0:
		raw = string(tpl.Content)
	case tpl.Inline != "":
		raw = tpl.Inline
	case tpl.FS != nil && tpl.Path != "":
		b, err := fs.ReadFile(tpl.FS, tpl.Path)
		if err != nil {
			return nil, sdk.NewError(sdk.CodeTemplateRenderFailed, "TPL", http.StatusUnprocessableEntity, sdk.CodeTemplateRenderFailed, false,
				fmt.Errorf("read template %s: %w", tpl.Path, err))
		}
		raw = string(b)
	default:
		return nil, sdk.NewError(sdk.CodeTemplateRenderFailed, "TPL", http.StatusUnprocessableEntity, sdk.CodeTemplateRenderFailed, false,
			fmt.Errorf("empty template reference"))
	}

	fm := e.funcMap(ctx, registerSecret)
	name := tpl.Path
	if name == "" {
		name = "inline"
	}
	parsed, err := template.New(name).Option("missingkey=error").Funcs(fm).Parse(raw)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeTemplateRenderFailed, "TPL", http.StatusUnprocessableEntity, sdk.CodeTemplateRenderFailed, false, err)
	}
	var buf bytes.Buffer
	if err := parsed.Execute(&buf, vars); err != nil {
		return nil, sdk.NewError(sdk.CodeTemplateRenderFailed, "TPL", http.StatusUnprocessableEntity, sdk.CodeTemplateRenderFailed, false, err)
	}
	return buf.Bytes(), nil
}

// SelectVersionDir selects a semver-range subdirectory under component (05 §3.9).
func (e *Engine) SelectVersionDir(fsys fs.FS, component string, ver *semver.Version) (fs.FS, error) {
	entries, err := fs.ReadDir(fsys, component)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ent := range entries {
		if ent.IsDir() {
			names = append(names, ent.Name())
		}
	}
	sort.Strings(names)
	for _, dirName := range names {
		c, cerr := semver.NewConstraint(dirName)
		if cerr != nil {
			continue
		}
		if c.Check(ver) {
			return fs.Sub(fsys, path.Join(component, dirName))
		}
	}
	return nil, fmt.Errorf("no template version dir matching %s under %s", ver.String(), component)
}

// EvalBool evaluates a boolean expression safely via precompiled expr-lang/expr (ADR-036).
func (e *Engine) EvalBool(expression string, env map[string]any) (bool, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return true, nil
	}
	e.exprMu.RLock()
	prog, ok := e.exprCache[expression]
	e.exprMu.RUnlock()
	if !ok {
		var err error
		prog, err = expr.Compile(expression, expr.AsBool())
		if err != nil {
			return false, sdk.NewError(sdk.CodeTemplateRenderFailed, "TPL", http.StatusUnprocessableEntity, sdk.CodeTemplateRenderFailed, false,
				fmt.Errorf("compile expr %q: %w", expression, err))
		}
		e.exprMu.Lock()
		e.exprCache[expression] = prog
		e.exprMu.Unlock()
	}
	out, err := expr.Run(prog, env)
	if err != nil {
		return false, sdk.NewError(sdk.CodeTemplateRenderFailed, "TPL", http.StatusUnprocessableEntity, sdk.CodeTemplateRenderFailed, false,
			fmt.Errorf("eval expr %q: %w", expression, err))
	}
	b, _ := out.(bool)
	return b, nil
}

func (e *Engine) funcMap(ctx context.Context, registerSecret func(string)) template.FuncMap {
	return template.FuncMap{
		"default": func(def, val any) any {
			if val == nil || val == "" || val == 0 || val == false {
				return def
			}
			return val
		},
		"quote":     func(s string) string { return fmt.Sprintf("%q", s) },
		"trim":      strings.TrimSpace,
		"lower":     strings.ToLower,
		"upper":     strings.ToUpper,
		"replace":   strings.ReplaceAll,
		"join":      strings.Join,
		"split":     strings.Split,
		"hasPrefix": strings.HasPrefix,
		"hasSuffix": strings.HasSuffix,
		"contains":  strings.Contains,
		"indent": func(spaces int, s string) string {
			pad := strings.Repeat(" ", spaces)
			return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
		},
		"nindent": func(spaces int, s string) string {
			pad := strings.Repeat(" ", spaces)
			return "\n" + pad + strings.ReplaceAll(s, "\n", "\n"+pad)
		},
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b int) int { return a * b },
		"div": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"min": func(a, b int) int {
			if a < b {
				return a
			}
			return b
		},
		"max": func(a, b int) int {
			if a > b {
				return a
			}
			return b
		},
		"b64enc": func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) },
		"sha256": func(s string) string {
			h := sha256.Sum256([]byte(s))
			return hex.EncodeToString(h[:])
		},
		"toJSON": func(v any) (string, error) {
			b, err := json.Marshal(v)
			return string(b), err
		},
		"toYAML": func(v any) (string, error) {
			b, err := yaml.Marshal(v)
			return strings.TrimRight(string(b), "\n"), err
		},
		// urlEncode (D2 §4.2): escapes special DSN password characters AND registers both raw & escaped forms for redaction!
		"urlEncode": func(s string) string {
			escaped := url.QueryEscape(s)
			if registerSecret != nil && s != "" {
				registerSecret(s)
				registerSecret(escaped)
			}
			return escaped
		},
		// secret looks up a named secret and registers it in RedactWriter automatically.
		"secret": func(name string) (string, error) {
			if e.secretLookup == nil {
				return "", fmt.Errorf("secret lookup unavailable for %q", name)
			}
			val, err := e.secretLookup(ctx, name)
			if err != nil {
				return "", err
			}
			if registerSecret != nil && val != "" {
				registerSecret(val)
				registerSecret(url.QueryEscape(val))
			}
			return val, nil
		},
	}
}
