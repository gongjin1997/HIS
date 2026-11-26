package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/kinstall/kinstall/internal/app"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/i18n"
	"github.com/kinstall/kinstall/internal/version"
	"github.com/kinstall/kinstall/pkg/client"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// ServerStarter is injected by cmd/kinstall so internal/cli never imports internal/server or internal/store.
type ServerStarter func(ctx context.Context, listenAddr string, devNoAuth bool) error

// Deps supplies dependencies from the composition root cmd/kinstall/app.go (O-04).
type Deps struct {
	Facade      *app.Facade
	StartServer ServerStarter
	Out         io.Writer
}

// NewRootCmd constructs the canonical kinstall Cobra command tree (05 §4.1, T-M0-17).
// Strictly zero business logic: delegates everything to internal/app.Facade (local) or pkg/client (--server).
func NewRootCmd(deps Deps) *cobra.Command {
	var (
		outputFmt string
		serverURL string
		token     string
		lang      string
		projectID string
	)
	out := deps.Out
	if out == nil {
		out = os.Stdout
	}
	cliActor := domain.Actor{
		ID: "cli_local", Username: "local-ops", Type: "cli",
		RoleBindings: []domain.RoleBinding{{ProjectID: "*", Role: domain.RoleAdmin}},
	}

	root := &cobra.Command{
		Use:           "kinstall",
		Short:         "kinstall — offline-first, agentless infrastructure & middleware installation platform",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&outputFmt, "output", "o", "table", "Output format: table | json | yaml")
	root.PersistentFlags().StringVar(&serverURL, "server", "", "Remote kinstall server URL (switches CLI to REST client mode)")
	root.PersistentFlags().StringVar(&token, "token", os.Getenv("KINSTALL_TOKEN"), "API token for --server mode")
	root.PersistentFlags().StringVar(&lang, "lang", "", "Language (zh-CN | en-US)")
	root.PersistentFlags().StringVarP(&projectID, "project", "p", "", "Project ID")

	render := func(v any, headers []string, rows [][]string) error {
		switch outputFmt {
		case "json":
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(v)
		case "yaml":
			b, err := yaml.Marshal(v)
			if err != nil {
				return err
			}
			_, err = out.Write(b)
			return err
		default:
			tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
			if len(headers) > 0 {
				for i, h := range headers {
					if i > 0 {
						_, _ = fmt.Fprint(tw, "\t")
					}
					_, _ = fmt.Fprint(tw, h)
				}
				_, _ = fmt.Fprintln(tw)
			}
			for _, row := range rows {
				for i, c := range row {
					if i > 0 {
						_, _ = fmt.Fprint(tw, "\t")
					}
					_, _ = fmt.Fprint(tw, c)
				}
				_, _ = fmt.Fprintln(tw)
			}
			return tw.Flush()
		}
	}

	// version
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print kinstall build and schema compatibility metadata",
		RunE: func(_ *cobra.Command, _ []string) error {
			info := version.Get()
			return render(info, []string{"VERSION", "COMMIT", "GO", "MAX_SCHEMA"}, [][]string{{info.Version, info.Commit, info.GoVersion, fmt.Sprintf("%d", info.MaxCompatibleSchemaVersion)}})
		},
	})

	// plugin list
	pluginCmd := &cobra.Command{Use: "plugin", Short: "Manage registered product plugins"}
	pluginCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all built-in and loaded plugins",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if serverURL != "" {
				c := client.New(serverURL, token)
				c.Lang = i18n.ResolveCLILang(lang)
				var resp struct {
					Items []sdk.PluginMeta `json:"items"`
				}
				if err := c.DoJSON(cmd.Context(), "GET", "/api/v1/plugins", nil, &resp, nil); err != nil {
					return err
				}
				return renderPlugins(render, resp.Items)
			}
			return renderPlugins(render, deps.Facade.ListPlugins())
		},
	})
	root.AddCommand(pluginCmd)

	// project create / list
	projectCmd := &cobra.Command{Use: "project", Short: "Manage projects"}
	var prjDesc string
	prjCreate := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := deps.Facade.CreateProject(cmd.Context(), cliActor, args[0], prjDesc)
			if err != nil {
				return err
			}
			return render(p, []string{"ID", "NAME", "DESCRIPTION"}, [][]string{{p.ID, p.Name, p.Description}})
		},
	}
	prjCreate.Flags().StringVar(&prjDesc, "desc", "", "Project description")
	projectCmd.AddCommand(prjCreate, &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := deps.Facade.ListProjects(cmd.Context(), cliActor)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(items))
			for _, p := range items {
				rows = append(rows, []string{p.ID, p.Name, p.Description})
			}
			return render(items, []string{"ID", "NAME", "DESCRIPTION"}, rows)
		},
	})
	root.AddCommand(projectCmd)

	// host add / list / probe
	hostCmd := &cobra.Command{Use: "host", Short: "Manage CMDB hosts and Facts"}
	var hAddr, hUser, hCred string
	var hPort int
	hostAdd := &cobra.Command{
		Use:   "add <name>",
		Short: "Register a host in CMDB",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := deps.Facade.CreateHost(cmd.Context(), cliActor, &domain.Host{
				ProjectID: projectID, Name: args[0], Address: hAddr, SSHPort: hPort, SSHUser: hUser, CredentialID: hCred,
			})
			if err != nil {
				return err
			}
			return render(h, []string{"ID", "NAME", "ADDRESS", "STATE"}, [][]string{{h.ID, h.Name, h.Address, string(h.State)}})
		},
	}
	hostAdd.Flags().StringVar(&hAddr, "address", "", "SSH host IP or DNS")
	hostAdd.Flags().IntVar(&hPort, "port", 22, "SSH port")
	hostAdd.Flags().StringVar(&hUser, "user", "root", "SSH username")
	hostAdd.Flags().StringVar(&hCred, "credential", "", "Credential ID")
	hostCmd.AddCommand(hostAdd, &cobra.Command{
		Use:   "list",
		Short: "List hosts in project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := deps.Facade.ListHosts(cmd.Context(), cliActor, projectID)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(items))
			for _, h := range items {
				rows = append(rows, []string{h.ID, h.Name, h.Address, h.Arch, fmt.Sprintf("%d", h.PageSize), string(h.State)})
			}
			return render(items, []string{"ID", "NAME", "ADDRESS", "ARCH", "PAGESIZE", "STATE"}, rows)
		},
	}, &cobra.Command{
		Use:   "probe <hostId>",
		Short: "Execute 1-RTT POSIX Facts probe on a host",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			facts, err := deps.Facade.ProbeHostFacts(cmd.Context(), cliActor, projectID, args[0])
			if err != nil {
				return err
			}
			return render(facts, []string{"HOSTNAME", "OS", "ARCH", "PAGESIZE", "GLIBC", "CGROUP"}, [][]string{{
				facts.Hostname, facts.OS.RuleID, facts.Arch, fmt.Sprintf("%d", facts.PageSize), facts.GlibcVersion, fmt.Sprintf("v%d", facts.CgroupVer),
			}})
		},
	})
	root.AddCommand(hostCmd)

	// instance list / plan / apply
	instCmd := &cobra.Command{Use: "instance", Short: "Manage product instances"}
	instCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List instances in project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := deps.Facade.ListInstances(cmd.Context(), cliActor, projectID)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(items))
			for _, it := range items {
				rows = append(rows, []string{it.ID, it.Name, it.Product, string(it.DeployTarget), it.ProductVersion, it.State})
			}
			return render(items, []string{"ID", "NAME", "PRODUCT", "TARGET", "VERSION", "STATE"}, rows)
		},
	})
	root.AddCommand(instCmd)

	// operation list / approve / reject
	opCmd := &cobra.Command{Use: "operation", Short: "Inspect and approve operations"}
	opCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List operations in project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := deps.Facade.ListOperations(cmd.Context(), cliActor, projectID, 50)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(items))
			for _, o := range items {
				rows = append(rows, []string{o.ID, o.TargetType + "/" + o.TargetID, o.Action, string(o.State), fmt.Sprintf("%d", o.FencingToken)})
			}
			return render(items, []string{"ID", "TARGET", "ACTION", "STATE", "FENCING"}, rows)
		},
	})
	root.AddCommand(opCmd)

	// audit verify
	auditCmd := &cobra.Command{Use: "audit", Short: "Audit hash-chain verification"}
	auditCmd.AddCommand(&cobra.Command{
		Use:   "verify",
		Short: "Verify SHA-256 / SM3 hash chain integrity across all audit_logs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := deps.Facade.VerifyAuditChain(cmd.Context(), cliActor)
			if err != nil {
				return err
			}
			return render(res, []string{"VALID", "CHECKED", "LAST_SEQ", "BROKEN_AT"}, [][]string{{
				fmt.Sprintf("%v", res.Valid), fmt.Sprintf("%d", res.TotalChecked), fmt.Sprintf("%d", res.LastSeq), fmt.Sprintf("%d", res.BrokenAtSeq),
			}})
		},
	})
	root.AddCommand(auditCmd)

	// compliance report
	compCmd := &cobra.Command{Use: "compliance", Short: "DJCP Level-3 & GM/T-0054 compliance evidence"}
	compCmd.AddCommand(&cobra.Command{
		Use:   "report",
		Short: "Generate compliance verification report & evidence digest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep, _, digest, err := deps.Facade.GenerateComplianceReport(cmd.Context(), cliActor, projectID)
			if err != nil {
				return err
			}
			return render(map[string]any{"report": rep, "sha256": digest}, []string{"STANDARD", "HOSTS", "INSTANCES", "EVIDENCE_SHA256"}, [][]string{{
				rep.Standard, fmt.Sprintf("%d", rep.HostCount), fmt.Sprintf("%d", rep.InstanceCount), digest[:16],
			}})
		},
	})
	root.AddCommand(compCmd)

	// server start
	serverCmd := &cobra.Command{Use: "server", Short: "Run the HA Control Plane HTTP/SSE Server + Web UI"}
	var listenAddr string
	var devNoAuth bool
	srvStart := &cobra.Command{
		Use:   "start",
		Short: "Start the kinstall HTTP server and background workers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deps.StartServer == nil {
				return fmt.Errorf("server starter not configured")
			}
			return deps.StartServer(cmd.Context(), listenAddr, devNoAuth)
		},
	}
	srvStart.Flags().StringVar(&listenAddr, "listen", ":8080", "HTTP listen address")
	srvStart.Flags().BoolVar(&devNoAuth, "dev-no-auth", false, "Allow unauthenticated local admin access (dev only)")
	serverCmd.AddCommand(srvStart)
	root.AddCommand(serverCmd)

	return root
}

func renderPlugins(render func(any, []string, [][]string) error, items []sdk.PluginMeta) error {
	rows := make([][]string, 0, len(items))
	for _, m := range items {
		rows = append(rows, []string{m.Name, m.Version, m.SDKVersion, string(m.Type), m.Description})
	}
	return render(items, []string{"NAME", "VERSION", "SDK", "TYPE", "DESCRIPTION"}, rows)
}
