package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// GoPluginHandshake defines the canonical hashicorp/go-plugin v1.8.0 handshake contract (T-M5-22, ADR-044/061).
var GoPluginHandshake = goplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "KINSTALL_EXTERNAL_PLUGIN",
	MagicCookieValue: "v1alpha1-grpc-sandbox",
}

// GoPluginName is the plugin map key registered on go-plugin GRPC servers/clients.
const GoPluginName = "kinstall-component"

// ExternalComponentService is the gRPC service interface served by external process plugins.
// Receives ONLY read-only InstanceView/HostView and returns ONLY declarative PipelineSpec (ZERO SSH/Secret exposure).
type ExternalComponentService interface {
	Meta(ctx context.Context) (sdk.PluginMeta, error)
	Validate(ctx context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error)
	Plan(ctx context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error)
	Status(ctx context.Context, req *sdk.StatusRequest) (*sdk.InstanceStatus, error)
}

const grpcServiceName = "kinstall.plugin.v1.ComponentService"

type grpcRawServer interface {
	Call(ctx context.Context, req *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error)
}

func grpcCallHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(wrapperspb.BytesValue)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(grpcRawServer).Call(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + grpcServiceName + "/Call"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(grpcRawServer).Call(ctx, req.(*wrapperspb.BytesValue))
	}
	return interceptor(ctx, in, info, handler)
}

var componentGRPCServiceDesc = grpc.ServiceDesc{
	ServiceName: grpcServiceName,
	HandlerType: (*grpcRawServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Call", Handler: grpcCallHandler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "kinstall_plugin_v1.proto",
}

type grpcPluginServerAdapter struct {
	impl ExternalComponentService
}

func (a *grpcPluginServerAdapter) Call(ctx context.Context, in *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error) {
	var req ExternalRPCRequest
	if err := json.Unmarshal(in.GetValue(), &req); err != nil {
		return nil, err
	}
	var out any
	var err error
	switch req.Method {
	case "meta":
		out, err = a.impl.Meta(ctx)
	case "validate":
		raw, _ := json.Marshal(req.Params)
		var vr sdk.ValidateRequest
		if uerr := json.Unmarshal(raw, &vr); uerr != nil {
			return nil, uerr
		}
		out, err = a.impl.Validate(ctx, &vr)
	case "plan":
		raw, _ := json.Marshal(req.Params)
		var pr sdk.PlanRequest
		if uerr := json.Unmarshal(raw, &pr); uerr != nil {
			return nil, uerr
		}
		out, err = a.impl.Plan(ctx, &pr)
	case "status":
		raw, _ := json.Marshal(req.Params)
		var sr sdk.StatusRequest
		if uerr := json.Unmarshal(raw, &sr); uerr != nil {
			return nil, uerr
		}
		out, err = a.impl.Status(ctx, &sr)
	default:
		return nil, fmt.Errorf("unknown external plugin method %q", req.Method)
	}
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return wrapperspb.Bytes(b), nil
}

// ComponentGRPCPlugin implements hashicorp/go-plugin.GRPCPlugin for both host and external plugin binaries.
type ComponentGRPCPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	Impl ExternalComponentService
}

func (p *ComponentGRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	s.RegisterService(&componentGRPCServiceDesc, &grpcPluginServerAdapter{impl: p.Impl})
	return nil
}

func (p *ComponentGRPCPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, cc *grpc.ClientConn) (any, error) {
	return &grpcComponentClient{cc: cc}, nil
}

// ServeExternalGRPCPlugin is the single entry point called by external plugin main() binaries (T-M5-22).
func ServeExternalGRPCPlugin(impl ExternalComponentService) {
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: GoPluginHandshake,
		Plugins: map[string]goplugin.Plugin{
			GoPluginName: &ComponentGRPCPlugin{Impl: impl},
		},
		GRPCServer: goplugin.DefaultGRPCServer,
	})
}

type grpcComponentClient struct {
	cc *grpc.ClientConn
}

func (c *grpcComponentClient) invoke(ctx context.Context, method string, params any, dst any) error {
	payload, err := json.Marshal(ExternalRPCRequest{Method: method, Params: params})
	if err != nil {
		return err
	}
	out := new(wrapperspb.BytesValue)
	if err := c.cc.Invoke(ctx, "/"+grpcServiceName+"/Call", wrapperspb.Bytes(payload), out); err != nil {
		return err
	}
	return json.Unmarshal(out.GetValue(), dst)
}

// GoPluginComponent wraps a live hashicorp/go-plugin v1.8.0 gRPC subprocess as sdk.Component.
type GoPluginComponent struct {
	client *goplugin.Client
	rpc    *grpcComponentClient
	meta   sdk.PluginMeta
}

// LoadGoPlugin launches an out-of-process gRPC plugin via hashicorp/go-plugin v1.8.0, verifies SDK compatibility,
// and enforces strict PipelineSpec-only task allowlisting (T-M5-22).
func LoadGoPlugin(ctx context.Context, binPath string) (*GoPluginComponent, error) {
	// #nosec G204 -- binPath is an explicitly configured external plugin binary registered by the operator
	cmd := exec.Command(binPath)
	cli := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: GoPluginHandshake,
		Plugins: map[string]goplugin.Plugin{
			GoPluginName: &ComponentGRPCPlugin{},
		},
		Cmd:              cmd,
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Logger:           hclog.NewNullLogger(),
	})
	rpcClient, err := cli.Client()
	if err != nil {
		cli.Kill()
		return nil, fmt.Errorf("connect go-plugin %s: %w", binPath, err)
	}
	raw, err := rpcClient.Dispense(GoPluginName)
	if err != nil {
		cli.Kill()
		return nil, fmt.Errorf("dispense go-plugin %s: %w", binPath, err)
	}
	gcli := raw.(*grpcComponentClient)
	var meta sdk.PluginMeta
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := gcli.invoke(cctx, "meta", nil, &meta); err != nil {
		cli.Kill()
		return nil, fmt.Errorf("fetch go-plugin meta: %w", err)
	}
	if meta.SDKVersion != sdk.SDKAPIVersion {
		cli.Kill()
		return nil, sdk.NewError(sdk.CodePluginIncompatible, "PLG", http.StatusUnprocessableEntity, sdk.CodePluginIncompatible, false,
			fmt.Errorf("go-plugin %s sdkVersion %q != %q", meta.Name, meta.SDKVersion, sdk.SDKAPIVersion))
	}
	return &GoPluginComponent{client: cli, rpc: gcli, meta: meta}, nil
}

// Kill terminates the isolated plugin subprocess without affecting the host control plane.
func (g *GoPluginComponent) Kill() {
	if g != nil && g.client != nil {
		g.client.Kill()
	}
}

func (g *GoPluginComponent) Meta() sdk.PluginMeta { return g.meta }

func (g *GoPluginComponent) Validate(ctx context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	var rep sdk.ValidationReport
	err := g.rpc.invoke(ctx, "validate", map[string]any{"instance": req.Instance, "hosts": req.Hosts}, &rep)
	return &rep, err
}

func (g *GoPluginComponent) Plan(ctx context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	var res sdk.PlanResult
	err := g.rpc.invoke(ctx, "plan", map[string]any{"instance": req.Instance, "hosts": req.Hosts, "operation": req.Operation}, &res)
	if err != nil {
		return nil, err
	}
	if res.Pipeline != nil {
		for _, ph := range res.Pipeline.Phases {
			for _, tk := range ph.Tasks {
				if !allowedBuiltinTaskTypes[tk.Type] {
					return nil, sdk.NewError(sdk.CodeEngUnknownTask, "PLG", http.StatusUnprocessableEntity, sdk.CodeEngUnknownTask, false,
						fmt.Errorf("go-plugin %s returned disallowed task type %q", g.meta.Name, tk.Type))
				}
			}
		}
	}
	return &res, nil
}

func (g *GoPluginComponent) Operation(_ string) (sdk.Operation, bool) { return nil, false }

func (g *GoPluginComponent) Status(ctx context.Context, req *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	var st sdk.InstanceStatus
	err := g.rpc.invoke(ctx, "status", map[string]any{"instance": req.Instance, "hosts": req.Hosts}, &st)
	return &st, err
}
