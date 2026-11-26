package osadapter

import (
	"fmt"
	"strings"

	"al.essio.dev/pkg/shellescape"

	"github.com/kinstall/kinstall/internal/ports"
)

// Adapter implements ports.OSAdapter backed by spec/osmatch.yaml and spec/pkgmap.yaml (D1 §4.2).
type Adapter struct {
	engine *Engine
	rule   *RuleSpec
}

// AdapterFor builds a strategy-backed OSAdapter for a matched RuleSpec.
func (e *Engine) AdapterFor(rule *RuleSpec) ports.OSAdapter {
	return &Adapter{engine: e, rule: rule}
}

func (a *Adapter) Family() string {
	if a.rule == nil {
		return "unknown"
	}
	return a.rule.Family
}

func (a *Adapter) RuleID() string {
	if a.rule == nil {
		return "unknown"
	}
	return a.rule.ID
}

func (a *Adapter) PkgType() string {
	return a.engine.RuleAttr(a.rule, "pkgType")
}

func (a *Adapter) PkgManager() string {
	return a.engine.RuleAttr(a.rule, "pkgManager")
}

func (a *Adapter) ResolvePkg(logical string) (string, bool, error) {
	return a.engine.ResolvePkg(logical, a.rule)
}

func (a *Adapter) PkgInstallCmd(pkgs []string) string {
	quoted := quoteList(pkgs)
	switch a.PkgManager() {
	case "apt":
		return "DEBIAN_FRONTEND=noninteractive apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends " + quoted
	case "zypper":
		return "zypper --non-interactive install --no-recommends " + quoted
	case "yum":
		return "yum install -y " + quoted
	default: // dnf
		return "dnf install -y " + quoted
	}
}

func (a *Adapter) PkgQueryCmd(pkgs []string) string {
	quoted := quoteList(pkgs)
	if a.PkgType() == "deb" {
		return "dpkg-query -W -f='${Status}\n' " + quoted + " 2>/dev/null | grep -q 'install ok installed'"
	}
	return "rpm -q " + quoted + " >/dev/null 2>&1"
}

// PkgLockCmd prevents OS upgrades from overwriting BOM-managed container/K8s packages (ADR-026).
func (a *Adapter) PkgLockCmd(pkgs []string) string {
	if len(pkgs) == 0 {
		return "true"
	}
	switch a.PkgManager() {
	case "apt":
		var sb strings.Builder
		sb.WriteString("cat <<'EOF' > /etc/apt/preferences.d/99-kinstall-exclude\n")
		for _, p := range pkgs {
			fmt.Fprintf(&sb, "Package: %s\nPin: release *\nPin-Priority: -1\n\n", p)
		}
		sb.WriteString("EOF")
		return sb.String()
	case "zypper":
		return "zypper --non-interactive addlock " + quoteList(pkgs)
	case "yum":
		return fmt.Sprintf("mkdir -p /etc/yum.conf.d && printf 'exclude=%s\\n' >> /etc/yum.conf", strings.Join(pkgs, " "))
	default:
		return fmt.Sprintf("mkdir -p /etc/dnf/dnf.conf.d && printf '[main]\\nexclude=%s\\n' > /etc/dnf/dnf.conf.d/99-kinstall-exclude.conf", strings.Join(pkgs, " "))
	}
}

func (a *Adapter) ServiceCmd(op ports.ServiceOp, unit string) string {
	u := shellescape.Quote(unit)
	switch op {
	case ports.ServiceStart:
		return "systemctl start " + u
	case ports.ServiceStop:
		return "systemctl stop " + u
	case ports.ServiceRestart:
		return "systemctl restart " + u
	case ports.ServiceReload:
		return "systemctl reload-or-restart " + u
	case ports.ServiceEnable:
		return "systemctl enable --now " + u
	case ports.ServiceDisable:
		return "systemctl disable --now " + u
	default:
		return "systemctl status " + u
	}
}

func (a *Adapter) FirewallBackend() string {
	return a.engine.RuleAttr(a.rule, "firewall")
}

func (a *Adapter) Bootloader() string {
	return a.engine.RuleAttr(a.rule, "bootloader")
}

func quoteList(items []string) string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = shellescape.Quote(s)
	}
	return strings.Join(out, " ")
}
