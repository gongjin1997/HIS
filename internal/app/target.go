package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/engine"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// BuildHostTarget constructs a live ports.Target for a CMDB host by decrypting its Vault credential
// and recursively resolving any bastion JumpHost chain (with cycle protection).
// Callers MUST invoke ZeroTargetAuth(&target) once connection setup finishes.
func BuildHostTarget(ctx context.Context, st ports.Store, sec ports.SecretStore, h *domain.Host) (ports.Target, error) {
	return buildHostTargetVisited(ctx, st, sec, h, map[string]bool{})
}

func buildHostTargetVisited(ctx context.Context, st ports.Store, sec ports.SecretStore, h *domain.Host, visited map[string]bool) (ports.Target, error) {
	if h == nil {
		return ports.Target{}, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("nil host"))
	}
	if visited[h.ID] {
		return ports.Target{}, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("jump host cycle detected at host %s (%s)", h.Name, h.ID))
	}
	visited[h.ID] = true

	port := h.SSHPort
	if port <= 0 {
		port = 22
	}
	user := h.SSHUser
	if user == "" {
		user = "root"
	}
	hkMode := ports.HostKeyMode(h.HostKeyMode)
	if hkMode == "" {
		hkMode = ports.HostKeyTOFU
	}
	sudoMode := ports.SudoNoPasswd
	if user == "root" {
		sudoMode = ports.SudoNone
	}

	target := ports.Target{
		HostID:      h.ID,
		Addr:        h.Address,
		Port:        port,
		User:        user,
		HostKeyMode: hkMode,
		HostKeyFP:   h.HostKeyFP,
		LegacyRSA:   h.LegacyRSA,
		SudoMode:    sudoMode,
		WorkDir:     h.WorkDir,
	}
	if st != nil {
		target.OnHostKeyTOFU = func(tctx context.Context, hid, fp string) error {
			return st.Hosts().UpdateHostKey(tctx, hid, fp)
		}
	}

	if h.CredentialID != "" && st != nil && sec != nil {
		cred, err := st.Credentials().Get(ctx, h.CredentialID)
		if err != nil {
			return ports.Target{}, fmt.Errorf("load credential %s for host %s: %w", h.CredentialID, h.Name, err)
		}
		plain, err := sec.Decrypt(ctx, "credentials", cred.ID, cred.KeyVersion, cred.EnvelopeJSON)
		if err != nil {
			return ports.Target{}, fmt.Errorf("decrypt credential %s for host %s: %w", cred.ID, h.Name, err)
		}
		populateAuthMaterial(&target.Auth, cred.Kind, plain)
		if user != "root" && len(target.Auth.SudoPassword) > 0 {
			target.SudoMode = ports.SudoPassword
		}
		clear(plain)
	}

	if h.JumpHostID != nil && strings.TrimSpace(*h.JumpHostID) != "" && st != nil {
		jh, err := st.Hosts().Get(ctx, strings.TrimSpace(*h.JumpHostID))
		if err != nil {
			ZeroTargetAuth(&target)
			return ports.Target{}, fmt.Errorf("load jump host %s for host %s: %w", *h.JumpHostID, h.Name, err)
		}
		jt, err := buildHostTargetVisited(ctx, st, sec, jh, visited)
		if err != nil {
			ZeroTargetAuth(&target)
			return ports.Target{}, err
		}
		// Flatten outer jumps first so sshx dials bastion chain in exact order
		target.Jumps = append(jt.Jumps, jt)
		target.Jumps[len(target.Jumps)-1].Jumps = nil
	}

	return target, nil
}

// ZeroTargetAuth wipes secret material across the target and every jump host in its chain.
func ZeroTargetAuth(t *ports.Target) {
	if t == nil {
		return
	}
	t.Auth.Zero()
	for i := range t.Jumps {
		t.Jumps[i].Auth.Zero()
	}
}

func populateAuthMaterial(auth *ports.AuthMaterial, kind string, plain []byte) {
	if len(plain) == 0 {
		return
	}
	trimmed := strings.TrimSpace(string(plain))
	// Support structured JSON credentials: {"privateKey":"...","passphrase":"...","password":"...","sudoPassword":"..."}
	if strings.HasPrefix(trimmed, "{") {
		var obj struct {
			PrivateKey   string `json:"privateKey"`
			Passphrase   string `json:"passphrase"`
			Password     string `json:"password"`
			SudoPassword string `json:"sudoPassword"`
		}
		if err := json.Unmarshal([]byte(trimmed), &obj); err == nil && (obj.PrivateKey != "" || obj.Password != "" || obj.SudoPassword != "") {
			if obj.PrivateKey != "" {
				auth.PrivateKey = []byte(obj.PrivateKey)
			}
			if obj.Passphrase != "" {
				auth.Passphrase = []byte(obj.Passphrase)
			}
			if obj.Password != "" {
				auth.Password = []byte(obj.Password)
			}
			if obj.SudoPassword != "" {
				auth.SudoPassword = []byte(obj.SudoPassword)
			}
			return
		}
	}
	switch strings.ToLower(kind) {
	case "ssh_key", "private_key":
		auth.PrivateKey = append([]byte(nil), plain...)
	default:
		// Detect PEM key even if kind was mislabeled
		if strings.Contains(trimmed, "-----BEGIN ") && strings.Contains(trimmed, "PRIVATE KEY-----") {
			auth.PrivateKey = append([]byte(nil), plain...)
		} else {
			auth.Password = append([]byte(nil), plain...)
			auth.SudoPassword = append([]byte(nil), plain...)
		}
	}
}

// ResolveOperationHosts resolves CMDB hosts + decrypted SSH targets for a Worker phase based on op.TargetType and HostSelector.
func ResolveOperationHosts(ctx context.Context, st ports.Store, sec ports.SecretStore, op *domain.Operation, sel sdk.HostSelector) ([]engine.HostExecBinding, error) {
	if st == nil || op == nil {
		return nil, nil
	}

	var candidates []domain.Host
	hostRoleMap := map[string][]string{}

	switch op.TargetType {
	case "host":
		h, err := st.Hosts().Get(ctx, op.TargetID)
		if err != nil {
			return nil, err
		}
		candidates = []domain.Host{*h}

	case "instance":
		inst, err := st.Instances().Get(ctx, op.TargetID)
		if err != nil {
			return nil, err
		}
		// Kubernetes targets execute helm/kubectl against control-plane/local target unless explicit hosts selected
		if inst.DeployTarget == sdk.TargetKubernetes && len(sel.HostIDs) == 0 && len(sel.Roles) == 0 {
			return nil, nil
		}
		allHosts, err := st.Hosts().ListByProject(ctx, op.ProjectID)
		if err != nil {
			return nil, err
		}
		// Extract optional targetHosts / hostRoles hints stored in DesiredSpecJSON or StateJSON
		preferredIDs, specRoles := extractInstanceHostHints(inst)
		for hid, roles := range specRoles {
			hostRoleMap[hid] = roles
		}
		if len(preferredIDs) > 0 {
			wanted := make(map[string]bool, len(preferredIDs))
			for _, id := range preferredIDs {
				wanted[id] = true
			}
			for _, h := range allHosts {
				if wanted[h.ID] || wanted[h.Name] {
					candidates = append(candidates, h)
				}
			}
		} else {
			candidates = allHosts
		}

	case "stack", "job":
		allHosts, err := st.Hosts().ListByProject(ctx, op.ProjectID)
		if err != nil {
			return nil, err
		}
		candidates = allHosts

	default:
		return nil, nil
	}

	filtered := filterHostCandidates(candidates, hostRoleMap, sel)
	if len(filtered) == 0 && len(sel.Roles) > 0 && len(candidates) > 0 {
		// Graceful fallback when roles weren't explicitly tagged on hosts (e.g. single-node cluster): use first candidate
		filtered = candidates[:1]
	}

	bindings := make([]engine.HostExecBinding, 0, len(filtered))
	for i := range filtered {
		h := filtered[i]
		target, err := BuildHostTarget(ctx, st, sec, &h)
		if err != nil {
			for j := range bindings {
				ZeroTargetAuth(&bindings[j].Target)
			}
			return nil, err
		}
		roles := hostRoleMap[h.ID]
		bindings = append(bindings, engine.HostExecBinding{
			View:   h.ToHostView(roles),
			Target: target,
		})
	}
	return bindings, nil
}

func extractInstanceHostHints(inst *domain.Instance) ([]string, map[string][]string) {
	roles := map[string][]string{}
	var ids []string
	parse := func(raw []byte) {
		if len(raw) == 0 {
			return
		}
		var obj struct {
			TargetHosts []string            `json:"targetHosts"`
			HostIDs     []string            `json:"hostIds"`
			HostRoles   map[string][]string `json:"hostRoles"`
		}
		if err := json.Unmarshal(raw, &obj); err == nil {
			ids = append(ids, obj.TargetHosts...)
			ids = append(ids, obj.HostIDs...)
			for k, v := range obj.HostRoles {
				roles[k] = v
			}
		}
	}
	parse(inst.DesiredSpecJSON)
	parse(inst.StateJSON)
	for k := range roles {
		ids = append(ids, k)
	}
	return ids, roles
}

func filterHostCandidates(candidates []domain.Host, hostRoleMap map[string][]string, sel sdk.HostSelector) []domain.Host {
	if len(sel.HostIDs) == 0 && len(sel.Roles) == 0 && len(sel.Labels) == 0 {
		return candidates
	}
	wantIDs := make(map[string]bool, len(sel.HostIDs))
	for _, id := range sel.HostIDs {
		wantIDs[id] = true
	}
	wantRoles := make(map[string]bool, len(sel.Roles))
	for _, r := range sel.Roles {
		wantRoles[strings.ToLower(r)] = true
	}

	var out []domain.Host
	for _, h := range candidates {
		if len(wantIDs) > 0 && !wantIDs[h.ID] && !wantIDs[h.Name] {
			continue
		}
		if len(sel.Labels) > 0 {
			matchAll := true
			for lk, lv := range sel.Labels {
				if h.Labels[lk] != lv {
					matchAll = false
					break
				}
			}
			if !matchAll {
				continue
			}
		}
		if len(wantRoles) > 0 {
			matched := false
			for _, r := range hostRoleMap[h.ID] {
				if wantRoles[strings.ToLower(r)] {
					matched = true
					break
				}
			}
			if !matched && h.Labels != nil {
				if roleLbl := h.Labels["role"]; roleLbl != "" {
					for _, part := range strings.Split(roleLbl, ",") {
						if wantRoles[strings.ToLower(strings.TrimSpace(part))] {
							matched = true
							break
						}
					}
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, h)
	}
	return out
}
