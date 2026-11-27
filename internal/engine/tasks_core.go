package engine

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"al.essio.dev/pkg/shellescape"

	"github.com/kinstall/kinstall/internal/planner"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func strParam(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[k]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func boolParam(m map[string]any, k string) bool {
	if m == nil {
		return false
	}
	b, _ := m[k].(bool)
	return b
}

func modeParam(m map[string]any, k string, def fs.FileMode) fs.FileMode {
	if m == nil {
		return def
	}
	switch v := m[k].(type) {
	case uint32:
		return fs.FileMode(v)
	case int:
		if v >= 0 && v <= 0o7777 {
			return fs.FileMode(v)
		}
		return def
	case float64:
		return fs.FileMode(uint32(v))
	default:
		return def
	}
}

func strSliceParam(m map[string]any, k string) []string {
	if m == nil {
		return nil
	}
	switch v := m[k].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, it := range v {
			out = append(out, fmt.Sprint(it))
		}
		return out
	default:
		return nil
	}
}

func resolveContent(ctx context.Context, rc *RunContext, spec sdk.TaskSpec, render bool) ([]byte, error) {
	if spec.Template == nil {
		if raw := strParam(spec.Params, "content"); raw != "" {
			return []byte(raw), nil
		}
		return nil, fmt.Errorf("missing template or content")
	}
	if !render {
		if len(spec.Template.Content) > 0 {
			return spec.Template.Content, nil
		}
		if spec.Template.Inline != "" {
			return []byte(spec.Template.Inline), nil
		}
		if spec.Template.FS != nil && spec.Template.Path != "" {
			return fs.ReadFile(spec.Template.FS, spec.Template.Path)
		}
	}
	if rc.Render == nil {
		return []byte(spec.Template.Inline), nil
	}
	vars := map[string]any{
		"Host": rc.Host,
	}
	for k, v := range spec.Vars {
		vars[k] = v
	}
	return rc.Render.Render(ctx, *spec.Template, vars, rc.RegisterSecret)
}

// --- #1 file.sync (3-Way Diff + create-only + managed-block + atomic upload + backup rollback) ---

type FileSyncTask struct{}

func (t *FileSyncTask) Name() string { return "file.sync" }

func (t *FileSyncTask) Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	return checkFileContent(ctx, rc, spec, false)
}

func (t *FileSyncTask) Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	return applyFileContent(ctx, rc, spec, false)
}

func (t *FileSyncTask) Rollback(ctx context.Context, rc *RunContext, spec sdk.TaskSpec, prev ApplyResult) error {
	return rollbackFileBackup(ctx, rc, spec, prev)
}

// --- #2 template.render ---

type TemplateRenderTask struct{}

func (t *TemplateRenderTask) Name() string { return "template.render" }

func (t *TemplateRenderTask) Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	return checkFileContent(ctx, rc, spec, true)
}

func (t *TemplateRenderTask) Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	return applyFileContent(ctx, rc, spec, true)
}

func (t *TemplateRenderTask) Rollback(ctx context.Context, rc *RunContext, spec sdk.TaskSpec, prev ApplyResult) error {
	return rollbackFileBackup(ctx, rc, spec, prev)
}

func checkFileContent(ctx context.Context, rc *RunContext, spec sdk.TaskSpec, render bool) (CheckResult, error) {
	dst := strParam(spec.Params, "dst")
	desired, err := resolveContent(ctx, rc, spec, render)
	if err != nil {
		return CheckResult{}, err
	}
	st, err := rc.Session.Stat(ctx, dst)
	if err != nil {
		return CheckResult{}, err
	}
	if spec.ManageMode == sdk.ManageModeManagedBlock && st.Exists && !st.IsDir {
		var cur bytes.Buffer
		if derr := rc.Session.Download(ctx, dst, &cur); derr == nil {
			merged := planner.MergeManagedBlock(cur.String(), spec.BlockID, string(desired))
			desired = []byte(merged)
		}
	}
	desiredSHA := planner.SHA256Hex(desired)
	baseSHA := ""
	if rc.LastApplied != nil {
		baseSHA = rc.LastApplied[dst]
	}
	// If no lastApplied recorded (M0 stateless / direct sync) and file matches desired, converge cleanly
	if baseSHA == "" && st.Exists && st.SHA256 == desiredSHA {
		return CheckResult{Converged: true, Summary: "unchanged"}, nil
	}
	adoptExisting := boolParam(spec.Params, "adoptExisting")
	if baseSHA == "" && !adoptExisting {
		// In M0/initial install without prior baseline, allow updating if adoptExisting not explicitly false
		adoptExisting = true
	}
	diff := planner.Evaluate3Way(dst, baseSHA, st.SHA256, desiredSHA, st.Exists, planner.DiffOptions{
		ManageMode:    spec.ManageMode,
		Adopt:         rc.Adopt,
		ForceOverride: rc.ForceOverride,
		AdoptExisting: adoptExisting,
	})
	if diff.Err != nil {
		return CheckResult{Diff: map[string]any{"scenario": diff.Scenario}}, diff.Err
	}
	if diff.Decision == planner.DiffSkipped || diff.Decision == planner.DiffConverged {
		if rc.LastApplied != nil {
			rc.LastApplied[dst] = desiredSHA
		}
		return CheckResult{Converged: true, Summary: string(diff.Decision)}, nil
	}
	return CheckResult{Converged: false, Summary: string(diff.Decision), Diff: map[string]any{"desiredSha": desiredSHA}}, nil
}

func applyFileContent(ctx context.Context, rc *RunContext, spec sdk.TaskSpec, render bool) (ApplyResult, error) {
	dst := strParam(spec.Params, "dst")
	mode := modeParam(spec.Params, "mode", 0o644)
	owner := strParam(spec.Params, "owner")
	desired, err := resolveContent(ctx, rc, spec, render)
	if err != nil {
		return ApplyResult{}, err
	}
	st, err := rc.Session.Stat(ctx, dst)
	if err != nil {
		return ApplyResult{}, err
	}
	backupRef := map[string]any{"dst": dst, "existed": st.Exists}
	if st.Exists && !st.IsDir {
		if spec.ManageMode == sdk.ManageModeManagedBlock {
			var cur bytes.Buffer
			if derr := rc.Session.Download(ctx, dst, &cur); derr == nil {
				desired = []byte(planner.MergeManagedBlock(cur.String(), spec.BlockID, string(desired)))
			}
		}
		// Snapshot existing file before mutation (Class A rollback anchor, 18 §2.7)
		bkPath := path.Join("/var/lib/kinstall/backups", rc.OpID, path.Base(dst)+".bak")
		cpCmd := fmt.Sprintf("mkdir -p %s && cp -a %s %s", shellescape.Quote(path.Dir(bkPath)), shellescape.Quote(dst), shellescape.Quote(bkPath))
		if _, cerr := rc.Session.Run(ctx, ports.Command{Script: cpCmd, Sudo: true}); cerr == nil {
			backupRef["backupPath"] = bkPath
		}
	}
	sha, err := rc.Session.Upload(ctx, bytes.NewReader(desired), dst, mode, owner)
	if err != nil {
		return ApplyResult{}, err
	}
	if rc.LastApplied != nil {
		rc.LastApplied[dst] = sha
	}
	if rc.LastAppliedContent != nil {
		rc.LastAppliedContent[dst] = string(desired)
	}
	if spec.Notify != "" && rc.Notify != nil {
		rc.Notify(spec.Notify)
	}
	return ApplyResult{Changed: true, BackupRef: backupRef, Output: sha}, nil
}

func rollbackFileBackup(ctx context.Context, rc *RunContext, _ sdk.TaskSpec, prev ApplyResult) error {
	dst := strParam(prev.BackupRef, "dst")
	if dst == "" {
		return nil
	}
	if bk := strParam(prev.BackupRef, "backupPath"); bk != "" {
		_, err := rc.Session.Run(ctx, ports.Command{
			Script: fmt.Sprintf("mv -f %s %s", shellescape.Quote(bk), shellescape.Quote(dst)),
			Sudo:   true,
		})
		return err
	}
	if existed, _ := prev.BackupRef["existed"].(bool); !existed {
		_, err := rc.Session.Run(ctx, ports.Command{Script: "rm -f " + shellescape.Quote(dst), Sudo: true})
		return err
	}
	return nil
}

// --- #3 pkg.ensure ---

type PkgEnsureTask struct{}

func (t *PkgEnsureTask) Name() string { return "pkg.ensure" }

func (t *PkgEnsureTask) resolvePackages(rc *RunContext, spec sdk.TaskSpec) ([]string, error) {
	logicals := strSliceParam(spec.Params, "packages")
	if rc.OS == nil {
		return logicals, nil
	}
	var resolved []string
	for _, lg := range logicals {
		pkg, missing, err := rc.OS.ResolvePkg(lg)
		if err != nil {
			return nil, err
		}
		if missing {
			return nil, sdk.NewError(sdk.CodeOSPkgMissing, "OS", http.StatusUnprocessableEntity, sdk.CodeOSPkgMissing, false,
				fmt.Errorf("logical package %q missing for OS rule %s", lg, rc.OS.RuleID()))
		}
		resolved = append(resolved, pkg)
	}
	return resolved, nil
}

func (t *PkgEnsureTask) Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	pkgs, err := t.resolvePackages(rc, spec)
	if err != nil {
		return CheckResult{}, err
	}
	if len(pkgs) == 0 {
		return CheckResult{Converged: true}, nil
	}
	qCmd := "true"
	if rc.OS != nil {
		qCmd = rc.OS.PkgQueryCmd(pkgs)
	}
	res, err := rc.Session.Run(ctx, ports.Command{Script: qCmd})
	if err == nil && res.ExitCode == 0 {
		return CheckResult{Converged: true, Summary: "packages present"}, nil
	}
	return CheckResult{Converged: false, Summary: "install " + strings.Join(pkgs, ",")}, nil
}

func (t *PkgEnsureTask) Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	pkgs, err := t.resolvePackages(rc, spec)
	if err != nil {
		return ApplyResult{}, err
	}
	if len(pkgs) == 0 {
		return ApplyResult{Changed: false}, nil
	}
	installCmd := "true"
	if rc.OS != nil {
		installCmd = rc.OS.PkgInstallCmd(pkgs)
	}
	res, err := rc.Session.Run(ctx, ports.Command{Script: installCmd, Sudo: true, Async: spec.Async, TaskRunID: rc.TaskRunID})
	if err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Changed: true, Output: res.Stdout}, nil
}

func (t *PkgEnsureTask) Rollback(context.Context, *RunContext, sdk.TaskSpec, ApplyResult) error {
	return nil // best-effort non-reversible for OS package upgrades (D2 §5.1 #3)
}

// --- #4 binary.install ---

type BinaryInstallTask struct{}

func (t *BinaryInstallTask) Name() string { return "binary.install" }

func (t *BinaryInstallTask) targetPaths(spec sdk.TaskSpec) (versionedPath, currentLink string) {
	name := strParam(spec.Params, "name")
	ver := strParam(spec.Params, "version")
	base := strParam(spec.Params, "installBase")
	if base == "" {
		base = "/opt/kinstall/bin"
	}
	return path.Join(base, name, ver, name), path.Join(base, name, "current")
}

func (t *BinaryInstallTask) Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	vPath, curLink := t.targetPaths(spec)
	expectedSHA := strParam(spec.Params, "sha256")
	st, err := rc.Session.Stat(ctx, vPath)
	if err != nil {
		return CheckResult{}, err
	}
	if !st.Exists || st.IsDir {
		return CheckResult{Converged: false, Summary: "binary missing"}, nil
	}
	if expectedSHA != "" && st.SHA256 != expectedSHA {
		return CheckResult{Converged: false, Summary: "binary sha256 mismatch"}, nil
	}
	// Check symlink points to current version dir
	res, err := rc.Session.Run(ctx, ports.Command{Script: fmt.Sprintf("readlink -f %s 2>/dev/null || true", shellescape.Quote(curLink))})
	if err == nil && strings.TrimSpace(res.Stdout) == path.Dir(vPath) {
		return CheckResult{Converged: true, Summary: "binary current"}, nil
	}
	return CheckResult{Converged: false, Summary: "update current symlink"}, nil
}

func (t *BinaryInstallTask) Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	vPath, curLink := t.targetPaths(spec)
	prevRes, _ := rc.Session.Run(ctx, ports.Command{Script: fmt.Sprintf("readlink -f %s 2>/dev/null || true", shellescape.Quote(curLink))})
	prevTarget := strings.TrimSpace(prevRes.Stdout)

	// Resolve source content either via MediaResolver (Local/OBS/Online) or inline test content
	var payload []byte
	var localPath string
	var expectedResolvedSHA string
	if content := strParam(spec.Params, "content"); content != "" {
		payload = []byte(content)
	} else if rc.Media != nil {
		resolved, err := rc.Media.Resolve(ctx, rc.MediaPolicy, sdk.MediaRef{
			Product:       strParam(spec.Params, "name"),
			Name:          strParam(spec.Params, "artifactName"),
			Version:       strParam(spec.Params, "version"),
			Arch:          rc.Host.Facts.Arch,
			PageSize:      rc.Host.Facts.PageSize,
			MatchPageSize: boolParam(spec.Params, "matchPageSize"),
			ExpectedSHA:   strParam(spec.Params, "sha256"),
		})
		if err != nil {
			return ApplyResult{}, err
		}
		if resolved.Presigned && rc.RegisterSecret != nil {
			rc.RegisterSecret(resolved.URL)
		}
		if resolved.URL != "" {
			// Channel 3: HTTP Range pull with rate limiting (D1 §2.5)
			dl := fmt.Sprintf(`set -e
mkdir -p %s
TMP=%s
if command -v curl >/dev/null 2>&1; then
  curl -fSL --retry 3 --limit-rate 50M -C - -o "$TMP" %s
else
  wget -q -c -O "$TMP" %s
fi
chmod 0755 "$TMP"
mv -f "$TMP" %s
ln -sfn %s %s`,
				shellescape.Quote(path.Dir(vPath)),
				shellescape.Quote(vPath+".tmp"),
				shellescape.Quote(resolved.URL),
				shellescape.Quote(resolved.URL),
				shellescape.Quote(vPath),
				shellescape.Quote(path.Dir(vPath)),
				shellescape.Quote(curLink),
			)
			if _, err := rc.Session.Run(ctx, ports.Command{Script: dl, Sudo: true, Async: spec.Async, TaskRunID: rc.TaskRunID, Stdout: rc.LogStdout, Stderr: rc.LogStderr}); err != nil {
				return ApplyResult{}, err
			}
			return ApplyResult{Changed: true, BackupRef: map[string]any{"curLink": curLink, "prevTarget": prevTarget}}, nil
		}
		if resolved.LocalPath != "" {
			localPath = resolved.LocalPath
			expectedResolvedSHA = resolved.SHA256
		}
	}

	if len(payload) > 0 {
		if _, err := rc.Session.Upload(ctx, bytes.NewReader(payload), vPath, 0o755, ""); err != nil {
			return ApplyResult{}, err
		}
	} else if localPath != "" {
		// Channel 1/2 (relay-cache): stream verified artifact directly from control-plane cache to host
		f, err := os.Open(localPath) // #nosec G304 -- path returned by PolicyMediaResolver from configured local/cache dir
		if err != nil {
			return ApplyResult{}, err
		}
		uploadedSHA, uerr := rc.Session.Upload(ctx, f, vPath, 0o755, "")
		_ = f.Close()
		if uerr != nil {
			return ApplyResult{}, uerr
		}
		if expectedResolvedSHA != "" && uploadedSHA != "" && !strings.EqualFold(uploadedSHA, expectedResolvedSHA) {
			return ApplyResult{}, sdk.NewError(sdk.CodeSrcDigestMismatch, "SRC", http.StatusUnprocessableEntity, sdk.CodeSrcDigestMismatch, false,
				fmt.Errorf("uploaded binary %s sha256 mismatch: got %s want %s", vPath, uploadedSHA, expectedResolvedSHA))
		}
	} else {
		// Ensure versioned directory exists when binary already staged
		_, _ = rc.Session.Run(ctx, ports.Command{Script: "mkdir -p " + shellescape.Quote(path.Dir(vPath)), Sudo: true})
	}
	linkCmd := fmt.Sprintf("ln -sfn %s %s", shellescape.Quote(path.Dir(vPath)), shellescape.Quote(curLink))
	if _, err := rc.Session.Run(ctx, ports.Command{Script: linkCmd, Sudo: true, Stdout: rc.LogStdout, Stderr: rc.LogStderr}); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Changed: true, BackupRef: map[string]any{"curLink": curLink, "prevTarget": prevTarget}}, nil
}

func (t *BinaryInstallTask) Rollback(ctx context.Context, rc *RunContext, _ sdk.TaskSpec, prev ApplyResult) error {
	curLink := strParam(prev.BackupRef, "curLink")
	prevTarget := strParam(prev.BackupRef, "prevTarget")
	if curLink != "" && prevTarget != "" {
		_, err := rc.Session.Run(ctx, ports.Command{Script: fmt.Sprintf("ln -sfn %s %s", shellescape.Quote(prevTarget), shellescape.Quote(curLink)), Sudo: true})
		return err
	}
	return nil
}

// --- #5 systemd.unit ---

type SystemdUnitTask struct{}

func (t *SystemdUnitTask) Name() string { return "systemd.unit" }

func (t *SystemdUnitTask) Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	unitName := strParam(spec.Params, "name")
	unitPath := path.Join("/etc/systemd/system", unitName)
	content, err := resolveContent(ctx, rc, spec, true)
	if err != nil {
		return CheckResult{}, err
	}
	st, err := rc.Session.Stat(ctx, unitPath)
	if err != nil {
		return CheckResult{}, err
	}
	if !st.Exists || st.SHA256 != planner.SHA256Hex(content) {
		return CheckResult{Converged: false, Summary: "unit file changed"}, nil
	}
	if boolParam(spec.Params, "start") {
		res, runErr := rc.Session.Run(ctx, ports.Command{Script: "systemctl is-active " + shellescape.Quote(unitName)})
		if runErr != nil || strings.TrimSpace(res.Stdout) != "active" {
			return CheckResult{Converged: false, Summary: "unit inactive"}, nil //nolint:nilerr // non-zero is-active means unit is not yet active
		}
	}
	return CheckResult{Converged: true, Summary: "unit active and unchanged"}, nil
}

func (t *SystemdUnitTask) Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	unitName := strParam(spec.Params, "name")
	unitPath := path.Join("/etc/systemd/system", unitName)
	content, err := resolveContent(ctx, rc, spec, true)
	if err != nil {
		return ApplyResult{}, err
	}
	if _, err := rc.Session.Upload(ctx, bytes.NewReader(content), unitPath, 0o644, ""); err != nil {
		return ApplyResult{}, err
	}
	var cmds []string
	cmds = append(cmds, "systemctl daemon-reload")
	if boolParam(spec.Params, "enable") {
		cmds = append(cmds, "systemctl enable "+shellescape.Quote(unitName))
	}
	if boolParam(spec.Params, "restartOnChange") {
		cmds = append(cmds, "systemctl restart "+shellescape.Quote(unitName))
	} else if boolParam(spec.Params, "start") {
		cmds = append(cmds, "systemctl start "+shellescape.Quote(unitName))
	}
	if _, err := rc.Session.Run(ctx, ports.Command{Script: strings.Join(cmds, " && "), Sudo: true}); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Changed: true}, nil
}

func (t *SystemdUnitTask) Rollback(context.Context, *RunContext, sdk.TaskSpec, ApplyResult) error {
	return nil
}

// --- #6 sysctl.apply ---

type SysctlApplyTask struct{}

func (t *SysctlApplyTask) Name() string { return "sysctl.apply" }

func (t *SysctlApplyTask) renderDropIn(spec sdk.TaskSpec) (string, []byte) {
	dropIn := strParam(spec.Params, "dropIn")
	if dropIn == "" {
		dropIn = "99-kinstall.conf"
	}
	dst := dropIn
	if !strings.HasPrefix(dst, "/") {
		dst = path.Join("/etc/sysctl.d", dropIn)
	}
	params := map[string]string{}
	switch p := spec.Params["params"].(type) {
	case map[string]string:
		params = p
	case map[string]any:
		for k, v := range p {
			params[k] = fmt.Sprint(v)
		}
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("# Managed by kinstall\n")
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s = %s\n", k, params[k])
	}
	return dst, []byte(sb.String())
}

func (t *SysctlApplyTask) Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	dst, content := t.renderDropIn(spec)
	st, err := rc.Session.Stat(ctx, dst)
	if err != nil {
		return CheckResult{}, err
	}
	if st.Exists && st.SHA256 == planner.SHA256Hex(content) {
		return CheckResult{Converged: true, Summary: "sysctl drop-in matches"}, nil
	}
	return CheckResult{Converged: false, Summary: "apply sysctl " + dst}, nil
}

func (t *SysctlApplyTask) Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	dst, content := t.renderDropIn(spec)
	if _, err := rc.Session.Upload(ctx, bytes.NewReader(content), dst, 0o644, ""); err != nil {
		return ApplyResult{}, err
	}
	if _, err := rc.Session.Run(ctx, ports.Command{Script: "sysctl -p " + shellescape.Quote(dst) + " || sysctl --system", Sudo: true}); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Changed: true}, nil
}

func (t *SysctlApplyTask) Rollback(context.Context, *RunContext, sdk.TaskSpec, ApplyResult) error {
	return nil
}

// --- #7 kmod.ensure ---

type KmodEnsureTask struct{}

func (t *KmodEnsureTask) Name() string { return "kmod.ensure" }

func (t *KmodEnsureTask) Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	mods := strSliceParam(spec.Params, "modules")
	if len(mods) == 0 {
		return CheckResult{Converged: true}, nil
	}
	var checks []string
	for _, m := range mods {
		checks = append(checks, fmt.Sprintf("test -d /sys/module/%s", shellescape.Quote(m)))
	}
	res, err := rc.Session.Run(ctx, ports.Command{Script: strings.Join(checks, " && ")})
	if err == nil && res.ExitCode == 0 {
		return CheckResult{Converged: true, Summary: "modules loaded"}, nil
	}
	return CheckResult{Converged: false, Summary: "load modules"}, nil
}

func (t *KmodEnsureTask) Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	mods := strSliceParam(spec.Params, "modules")
	if len(mods) == 0 {
		return ApplyResult{}, nil
	}
	content := strings.Join(mods, "\n") + "\n"
	if _, err := rc.Session.Upload(ctx, strings.NewReader(content), "/etc/modules-load.d/kinstall.conf", 0o644, ""); err != nil {
		return ApplyResult{}, err
	}
	var cmds []string
	for _, m := range mods {
		cmds = append(cmds, "modprobe "+shellescape.Quote(m))
	}
	if _, err := rc.Session.Run(ctx, ports.Command{Script: strings.Join(cmds, " && "), Sudo: true}); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Changed: true}, nil
}

func (t *KmodEnsureTask) Rollback(context.Context, *RunContext, sdk.TaskSpec, ApplyResult) error {
	return nil // never rmmod in-use kernel modules (D2 §5.1 #7)
}

// --- #8 cmd.run ---

type CmdRunTask struct{}

func (t *CmdRunTask) Name() string { return "cmd.run" }

func (t *CmdRunTask) Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	checkCmd := strParam(spec.Params, "check")
	if checkCmd == "" {
		if creates := strParam(spec.Params, "creates"); creates != "" {
			st, err := rc.Session.Stat(ctx, creates)
			if err == nil && st.Exists {
				return CheckResult{Converged: true, Summary: "creates path exists"}, nil
			}
		}
		return CheckResult{Converged: false, Summary: "run command"}, nil
	}
	res, err := rc.Session.Run(ctx, ports.Command{Script: checkCmd, Sudo: true})
	if err == nil && res.ExitCode == 0 {
		return CheckResult{Converged: true, Summary: "check passed"}, nil
	}
	return CheckResult{Converged: false, Summary: "check returned non-zero"}, nil
}

func (t *CmdRunTask) Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	script := strParam(spec.Params, "script")
	res, err := rc.Session.Run(ctx, ports.Command{
		Script:    script,
		Sudo:      true,
		Async:     spec.Async,
		Timeout:   spec.Timeout,
		TaskRunID: rc.TaskRunID,
		Stdout:    rc.LogStdout,
		Stderr:    rc.LogStderr,
	})
	if err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Changed: true, Output: res.Stdout}, nil
}

func (t *CmdRunTask) Rollback(ctx context.Context, rc *RunContext, spec sdk.TaskSpec, _ ApplyResult) error {
	if spec.RollbackCmd == "" {
		return nil
	}
	_, err := rc.Session.Run(ctx, ports.Command{Script: spec.RollbackCmd, Sudo: true})
	return err
}

// --- #9 wait.tcp ---

type WaitTCPTask struct{}

func (t *WaitTCPTask) Name() string { return "wait.tcp" }

func (t *WaitTCPTask) Check(ctx context.Context, _ *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	addr := strParam(spec.Params, "hostPort")
	d := net.Dialer{Timeout: 1 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err == nil {
		_ = conn.Close()
		return CheckResult{Converged: true, Summary: "tcp reachable"}, nil
	}
	return CheckResult{Converged: false, Summary: "waiting for tcp " + addr}, nil
}

func (t *WaitTCPTask) Apply(ctx context.Context, _ *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	addr := strParam(spec.Params, "hostPort")
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	d := net.Dialer{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return ApplyResult{Changed: false, Output: "reachable"}, nil
		}
		select {
		case <-ctx.Done():
			return ApplyResult{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return ApplyResult{}, sdk.NewError(sdk.CodeWaitTimeout, "WAIT", http.StatusGatewayTimeout, sdk.CodeWaitTimeout, true,
		fmt.Errorf("tcp probe timeout for %s after %s", addr, timeout))
}

func (t *WaitTCPTask) Rollback(context.Context, *RunContext, sdk.TaskSpec, ApplyResult) error {
	return nil
}

// --- #10 wait.http ---

type WaitHTTPTask struct{}

func (t *WaitHTTPTask) Name() string { return "wait.http" }

func (t *WaitHTTPTask) probe(ctx context.Context, urlStr string) bool {
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}, //nolint:gosec // local probe to self-signed cluster endpoints
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func (t *WaitHTTPTask) Check(ctx context.Context, _ *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	if t.probe(ctx, strParam(spec.Params, "url")) {
		return CheckResult{Converged: true, Summary: "http healthy"}, nil
	}
	return CheckResult{Converged: false, Summary: "waiting for http"}, nil
}

func (t *WaitHTTPTask) Apply(ctx context.Context, _ *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	u := strParam(spec.Params, "url")
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if t.probe(ctx, u) {
			return ApplyResult{Changed: false, Output: "healthy"}, nil
		}
		select {
		case <-ctx.Done():
			return ApplyResult{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return ApplyResult{}, sdk.NewError(sdk.CodeWaitTimeout, "WAIT", http.StatusGatewayTimeout, sdk.CodeWaitTimeout, true,
		fmt.Errorf("http probe timeout for %s after %s", u, timeout))
}

func (t *WaitHTTPTask) Rollback(context.Context, *RunContext, sdk.TaskSpec, ApplyResult) error {
	return nil
}
