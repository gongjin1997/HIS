package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// ExternalRPCRequest is the sandboxed JSON-RPC envelope passed to out-of-process external plugins (T-M5-10, R6).
// External plugins receive ONLY read-only InstanceView/HostView and return ONLY declarative PipelineSpec;
// they NEVER receive SSH keys, DB connections, or plaintext secrets!
type ExternalRPCRequest struct {
	Method string `json:"method"` // meta | validate | plan | status
	Params any    `json:"params,omitempty"`
}

type ExternalRPCResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// ExternalPluginProxy invokes an external plugin binary via JSON-RPC over stdin/stdout with strict task validation.
type ExternalPluginProxy struct {
	binPath string
	meta    sdk.PluginMeta
	timeout time.Duration
}

// LoadExternalPlugin handshakes with an external executable (`method: "meta"`) and verifies SDK compatibility.
func LoadExternalPlugin(ctx context.Context, binPath string, timeout time.Duration) (*ExternalPluginProxy, error) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	p := &ExternalPluginProxy{binPath: binPath, timeout: timeout}
	raw, err := p.call(ctx, "meta", nil)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &p.meta); err != nil {
		return nil, fmt.Errorf("decode external plugin meta: %w", err)
	}
	if p.meta.SDKVersion != sdk.SDKAPIVersion {
		return nil, sdk.NewError(sdk.CodePluginIncompatible, "PLG", http.StatusUnprocessableEntity, sdk.CodePluginIncompatible, false,
			fmt.Errorf("external plugin %s sdkVersion %q != %q", p.meta.Name, p.meta.SDKVersion, sdk.SDKAPIVersion))
	}
	return p, nil
}

func (p *ExternalPluginProxy) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	cctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	payload, err := json.Marshal(ExternalRPCRequest{Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	// #nosec G204 -- binPath is an explicitly configured plugin binary registered by the operator
	cmd := exec.CommandContext(cctx, p.binPath)
	cmd.Stdin = bytes.NewReader(payload)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("external plugin %s (%s) failed: %w: %s", p.binPath, method, err, errBuf.String())
	}
	var resp ExternalRPCResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("decode external plugin response: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("external plugin error: %s", resp.Error)
	}
	return resp.Result, nil
}

func (p *ExternalPluginProxy) Meta() sdk.PluginMeta { return p.meta }

func (p *ExternalPluginProxy) Validate(ctx context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	raw, err := p.call(ctx, "validate", map[string]any{"instance": req.Instance, "hosts": req.Hosts})
	if err != nil {
		return nil, err
	}
	var rep sdk.ValidationReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		return nil, err
	}
	return &rep, nil
}

func (p *ExternalPluginProxy) Plan(ctx context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	raw, err := p.call(ctx, "plan", map[string]any{"instance": req.Instance, "hosts": req.Hosts, "operation": req.Operation})
	if err != nil {
		return nil, err
	}
	var res sdk.PlanResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	if res.Pipeline != nil {
		for _, ph := range res.Pipeline.Phases {
			for _, tk := range ph.Tasks {
				if !allowedBuiltinTaskTypes[tk.Type] {
					return nil, sdk.NewError(sdk.CodeEngUnknownTask, "PLG", http.StatusUnprocessableEntity, sdk.CodeEngUnknownTask, false,
						fmt.Errorf("external plugin %s returned disallowed task type %q", p.meta.Name, tk.Type))
				}
			}
		}
	}
	return &res, nil
}

func (p *ExternalPluginProxy) Operation(_ string) (sdk.Operation, bool) { return nil, false }

func (p *ExternalPluginProxy) Status(ctx context.Context, req *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	raw, err := p.call(ctx, "status", map[string]any{"instance": req.Instance, "hosts": req.Hosts})
	if err != nil {
		return nil, err
	}
	var st sdk.InstanceStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	return &st, nil
}
