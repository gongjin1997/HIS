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
	"github.com/kinstall/kinstall/internal/artifact/source"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/i18n"
	"github.com/kinstall/kinstall/internal/inventory"
	"github.com/kinstall/kinstall/internal/version"
	"github.com/kinstall/kinstall/pkg/client"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// ServerStarter is injected by cmd/kinstall so internal/cli never imports internal/server or internal/store.
type ServerStarter func(ctx context.Context, listenAddr string, devNoAuth bool) error

// APIProxyRunner is injected by cmd/kinstall for `kinstall apiserver-proxy` (T-M1-20, P1 §3.1).
type APIProxyRunner func(ctx context.Context, listenAddr, configPath string) error

// Deps supplies dependencies from the composition root cmd/kinstall/app.go (O-04).
type Deps struct {
	Facade      *app.Facade
	StartServer ServerStarter
	RunAPIProxy APIProxyRunner
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

	newClient := func() *client.Client {
		c := client.New(serverURL, token)
		c.Lang = i18n.ResolveCLILang(lang)
		return c
	}

	// project create / list
	projectCmd := &cobra.Command{Use: "project", Short: "Manage projects"}
	var prjDesc string
	prjCreate := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if serverURL != "" {
				var p domain.Project
				if err := newClient().DoJSON(cmd.Context(), "POST", "/api/v1/projects", map[string]string{"name": args[0], "description": prjDesc}, &p, nil); err != nil {
					return err
				}
				return render(p, []string{"ID", "NAME", "DESCRIPTION"}, [][]string{{p.ID, p.Name, p.Description}})
			}
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
			var items []domain.Project
			if serverURL != "" {
				var resp struct {
					Items []domain.Project `json:"items"`
				}
				if err := newClient().DoJSON(cmd.Context(), "GET", "/api/v1/projects", nil, &resp, nil); err != nil {
					return err
				}
				items = resp.Items
			} else {
				var err error
				items, err = deps.Facade.ListProjects(cmd.Context(), cliActor)
				if err != nil {
					return err
				}
			}
			rows := make([][]string, 0, len(items))
			for _, p := range items {
				rows = append(rows, []string{p.ID, p.Name, p.Description})
			}
			return render(items, []string{"ID", "NAME", "DESCRIPTION"}, rows)
		},
	})
	root.AddCommand(projectCmd)

	// host add / list / probe / check / discover
	hostCmd := &cobra.Command{Use: "host", Short: "Manage CMDB hosts and Facts"}
	var hAddr, hUser, hCred string
	var hPort int
	hostAdd := &cobra.Command{
		Use:   "add <name>",
		Short: "Register a host in CMDB",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hIn := &domain.Host{
				ProjectID: projectID, Name: args[0], Address: hAddr, SSHPort: hPort, SSHUser: hUser, CredentialID: hCred,
			}
			if serverURL != "" {
				var outH domain.Host
				if err := newClient().DoJSON(cmd.Context(), "POST", "/api/v1/projects/"+projectID+"/hosts", hIn, &outH, nil); err != nil {
					return err
				}
				return render(outH, []string{"ID", "NAME", "ADDRESS", "STATE"}, [][]string{{outH.ID, outH.Name, outH.Address, string(outH.State)}})
			}
			h, err := deps.Facade.CreateHost(cmd.Context(), cliActor, hIn)
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
	var discPort int
	hostDiscover := &cobra.Command{
		Use:   "discover <cidr>",
		Short: "Scan CIDR subnet for reachable SSH endpoints (T-M1-04)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var found []inventory.DiscoveredHost
			if serverURL != "" {
				var resp struct {
					Items []inventory.DiscoveredHost `json:"items"`
				}
				if err := newClient().DoJSON(cmd.Context(), "POST", "/api/v1/projects/"+projectID+"/hosts:discover", map[string]any{"cidr": args[0], "port": discPort}, &resp, nil); err != nil {
					return err
				}
				found = resp.Items
			} else {
				var err error
				found, err = deps.Facade.DiscoverHostsSubnet(cmd.Context(), cliActor, projectID, args[0], discPort)
				if err != nil {
					return err
				}
			}
			rows := make([][]string, 0, len(found))
			for _, f := range found {
				rows = append(rows, []string{f.Address, fmt.Sprintf("%d", f.Port), fmt.Sprintf("%dms", f.LatencyMs), f.Banner})
			}
			return render(found, []string{"ADDRESS", "PORT", "LATENCY", "SSH_BANNER"}, rows)
		},
	}
	hostDiscover.Flags().IntVar(&discPort, "port", 22, "Target SSH port to probe")
	hostCmd.AddCommand(hostAdd, hostDiscover, &cobra.Command{
		Use:   "list",
		Short: "List hosts in project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var items []domain.Host
			if serverURL != "" {
				var resp struct {
					Items []domain.Host `json:"items"`
				}
				if err := newClient().DoJSON(cmd.Context(), "GET", "/api/v1/projects/"+projectID+"/hosts", nil, &resp, nil); err != nil {
					return err
				}
				items = resp.Items
			} else {
				var err error
				items, err = deps.Facade.ListHosts(cmd.Context(), cliActor, projectID)
				if err != nil {
					return err
				}
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
	}, &cobra.Command{
		Use:   "check <hostId>",
		Short: "Check CMDB host against its hostinit baseline profile (T-M1-08)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			drifts, err := deps.Facade.CheckHostBaseline(cmd.Context(), cliActor, projectID, args[0])
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(drifts))
			for _, d := range drifts {
				rows = append(rows, []string{d.Key, d.Expected, d.Observed})
			}
			return render(drifts, []string{"KEY", "EXPECTED", "OBSERVED"}, rows)
		},
	})
	root.AddCommand(hostCmd)

	// instance list / plan / apply / restore (T-M0-17, T-M4-11)
	instCmd := &cobra.Command{Use: "instance", Short: "Manage product instances"}
	var planAction, applyPlanID, restoreTargetTime string
	var forceOverride bool
	instPlan := &cobra.Command{
		Use:   "plan <instanceId>",
		Short: "Compute 3-Way Diff & frozen Plan snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if serverURL != "" {
				var resp map[string]any
				if err := newClient().DoJSON(cmd.Context(), "POST", "/api/v1/projects/"+projectID+"/instances/"+args[0]+":plan", map[string]string{"action": planAction}, &resp, nil); err != nil {
					return err
				}
				return render(resp, nil, nil)
			}
			pl, rep, err := deps.Facade.PlanInstance(cmd.Context(), cliActor, projectID, args[0], planAction, nil, nil)
			if err != nil {
				return err
			}
			return render(map[string]any{"plan": pl, "validation": rep}, []string{"PLAN_ID", "TARGET", "EXPIRES"}, [][]string{{pl.ID, pl.TargetID, pl.ExpiresAt.Format("2006-01-02T15:04:05Z")}})
		},
	}
	instPlan.Flags().StringVar(&planAction, "action", "install", "Lifecycle action (install|upgrade|scale|backup|restore|failover|uninstall)")

	instApply := &cobra.Command{
		Use:   "apply <instanceId>",
		Short: "Apply a frozen Plan (enforces four-eyes approval in prod)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if serverURL != "" {
				var op domain.Operation
				if err := newClient().DoJSON(cmd.Context(), "POST", "/api/v1/projects/"+projectID+"/instances/"+args[0]+":apply", map[string]any{"planId": applyPlanID, "action": planAction, "forceOverride": forceOverride}, &op, nil); err != nil {
					return err
				}
				return render(op, []string{"OP_ID", "ACTION", "STATE"}, [][]string{{op.ID, op.Action, string(op.State)}})
			}
			op, err := deps.Facade.ApplyInstance(cmd.Context(), cliActor, projectID, args[0], applyPlanID, planAction, forceOverride, false, nil)
			if err != nil {
				return err
			}
			return render(op, []string{"OP_ID", "ACTION", "STATE"}, [][]string{{op.ID, op.Action, string(op.State)}})
		},
	}
	instApply.Flags().StringVar(&applyPlanID, "plan", "", "Frozen Plan ID")
	instApply.Flags().StringVar(&planAction, "action", "install", "Lifecycle action")
	instApply.Flags().BoolVar(&forceOverride, "force-override", false, "Override remote drift (triggers four-eyes approval)")

	instRestore := &cobra.Command{
		Use:   "restore <instanceId>",
		Short: "Plan & trigger PITR point-in-time recovery (--target-time, T-M4-11)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pl, _, err := deps.Facade.PlanInstance(cmd.Context(), cliActor, projectID, args[0], "restore", nil, nil)
			if err != nil {
				return err
			}
			return render(map[string]any{"plan": pl, "targetTime": restoreTargetTime}, []string{"PLAN_ID", "TARGET_TIME"}, [][]string{{pl.ID, restoreTargetTime}})
		},
	}
	instRestore.Flags().StringVar(&restoreTargetTime, "target-time", "", "RFC3339 recovery point timestamp (e.g. 2026-11-27T12:00:00Z)")

	instCmd.AddCommand(instPlan, instApply, instRestore, &cobra.Command{
		Use:   "list",
		Short: "List instances in project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var items []domain.Instance
			if serverURL != "" {
				var resp struct {
					Items []domain.Instance `json:"items"`
				}
				if err := newClient().DoJSON(cmd.Context(), "GET", "/api/v1/projects/"+projectID+"/instances", nil, &resp, nil); err != nil {
					return err
				}
				items = resp.Items
			} else {
				var err error
				items, err = deps.Facade.ListInstances(cmd.Context(), cliActor, projectID)
				if err != nil {
					return err
				}
			}
			rows := make([][]string, 0, len(items))
			for _, it := range items {
				rows = append(rows, []string{it.ID, it.Name, it.Product, string(it.DeployTarget), it.ProductVersion, it.State})
			}
			return render(items, []string{"ID", "NAME", "PRODUCT", "TARGET", "VERSION", "STATE"}, rows)
		},
	})
	root.AddCommand(instCmd)

	// operation list / resume / report
	opCmd := &cobra.Command{Use: "operation", Short: "Inspect, resume, and report operations"}
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
	}, &cobra.Command{
		Use:   "resume <opId>",
		Short: "Resume a failed/paused operation from its last completed step (T-M0-12)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if serverURL != "" {
				var op domain.Operation
				if err := newClient().DoJSON(cmd.Context(), "POST", "/api/v1/projects/"+projectID+"/operations/"+args[0]+":resume", nil, &op, nil); err != nil {
					return err
				}
				return render(op, []string{"ID", "STATE"}, [][]string{{op.ID, string(op.State)}})
			}
			if err := deps.Facade.ResumeOperation(cmd.Context(), cliActor, args[0]); err != nil {
				return err
			}
			return render(map[string]string{"id": args[0], "state": "pending"}, []string{"ID", "STATE"}, [][]string{{args[0], "pending"}})
		},
	})
	var repFmt string
	opReport := &cobra.Command{
		Use:   "report <opId>",
		Short: "Export Markdown or JSON operation report (T-M2-05)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := deps.Facade.ExportOperationReport(cmd.Context(), cliActor, args[0], repFmt)
			if err != nil {
				return err
			}
			_, err = out.Write(raw)
			return err
		},
	}
	opReport.Flags().StringVar(&repFmt, "format", "markdown", "Report format: markdown | json")
	opCmd.AddCommand(opReport)
	root.AddCommand(opCmd)

	// secrets rewrap (T-M1-02 / O-11)
	secretsCmd := &cobra.Command{Use: "secrets", Short: "Manage envelope encryption keys & rotation"}
	secretsCmd.AddCommand(&cobra.Command{
		Use:   "rewrap",
		Short: "Re-wrap all credential DEKs in project under latest KEK version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			n, err := deps.Facade.RewrapAllCredentials(cmd.Context(), cliActor, projectID)
			if err != nil {
				return err
			}
			return render(map[string]int{"rewrapped": n}, []string{"REWRAPPED"}, [][]string{{fmt.Sprintf("%d", n)}})
		},
	})
	root.AddCommand(secretsCmd)

	// support-bundle (T-M2-05)
	var sbOut string
	sbCmd := &cobra.Command{
		Use:   "support-bundle",
		Short: "Collect a redacted diagnostic .tar.gz bundle (Redline 6)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			raw, rep, err := deps.Facade.CollectSupportBundle(cmd.Context(), cliActor, projectID)
			if err != nil {
				return err
			}
			if sbOut != "" {
				if err := os.WriteFile(sbOut, raw, 0o600); err != nil {
					return err
				}
			}
			return render(rep, []string{"HOSTS", "INSTANCES", "OPS", "BYTES"}, [][]string{{
				fmt.Sprintf("%d", rep.HostCount), fmt.Sprintf("%d", rep.InstanceCount), fmt.Sprintf("%d", rep.OperationCount), fmt.Sprintf("%d", rep.SizeBytes),
			}})
		},
	}
	sbCmd.Flags().StringVar(&sbOut, "out", "", "Write support-bundle .tar.gz to path")
	root.AddCommand(sbCmd)

	// spec export / diff (T-M3-11 GitOps)
	specCmd := &cobra.Command{Use: "spec", Short: "GitOps declarative export & diff (G2 §2)"}
	specCmd.AddCommand(&cobra.Command{
		Use:   "export",
		Short: "Export live project instances as deterministic GitOps YAML",
		RunE: func(cmd *cobra.Command, _ []string) error {
			raw, err := deps.Facade.ExportGitOpsYAML(cmd.Context(), cliActor, projectID)
			if err != nil {
				return err
			}
			_, err = out.Write(raw)
			return err
		},
	}, &cobra.Command{
		Use:   "diff <file.yaml>",
		Short: "Diff desired GitOps YAML against live instances",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			desired, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			entries, code, err := deps.Facade.DiffGitOpsYAML(cmd.Context(), cliActor, projectID, desired)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(entries))
			for _, e := range entries {
				rows = append(rows, []string{e.InstanceName, e.Action, e.BeforeHash, e.AfterHash})
			}
			_ = render(map[string]any{"exitCode": code, "entries": entries}, []string{"INSTANCE", "ACTION", "BEFORE", "AFTER"}, rows)
			return nil
		},
	})
	root.AddCommand(specCmd)

	// artifact verify-sources / sync-to-obs (T-M1-13)
	artCmd := &cobra.Command{Use: "artifact", Short: "Verify and sync ternary media sources (Local / OBS·S3 / Online)"}
	var obsEndpoint, obsBucket, obsRegion, obsPrefix, obsFile, obsKey, obsAK, obsSK string
	syncObsCmd := &cobra.Command{
		Use:   "sync-to-obs",
		Short: "Upload local artifact bundle to Huawei OBS / S3 via aws-sdk-go-v2 (no aws-chunked, T-M1-13)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := os.Open(obsFile)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			fi, err := f.Stat()
			if err != nil {
				return err
			}
			ak := obsAK
			if ak == "" {
				ak = os.Getenv("KINSTALL_OBS_AK")
			}
			sk := obsSK
			if sk == "" {
				sk = os.Getenv("KINSTALL_OBS_SK")
			}
			src := source.NewObjectStoreSource(domain.MediaSourceConfig{
				Type: "obs", Endpoint: obsEndpoint, Bucket: obsBucket, Region: obsRegion, Prefix: obsPrefix, Internal: true,
			}, ak, sk, nil)
			if err := src.PutObject(cmd.Context(), obsKey, f, fi.Size()); err != nil {
				return err
			}
			return render(map[string]any{"uploaded": obsKey, "bytes": fi.Size(), "bucket": obsBucket}, []string{"BUCKET", "KEY", "BYTES"}, [][]string{{obsBucket, obsKey, fmt.Sprintf("%d", fi.Size())}})
		},
	}
	syncObsCmd.Flags().StringVar(&obsEndpoint, "endpoint", "", "OBS/S3 endpoint URL")
	syncObsCmd.Flags().StringVar(&obsBucket, "bucket", "", "Target bucket name")
	syncObsCmd.Flags().StringVar(&obsRegion, "region", "cn-north-4", "Region")
	syncObsCmd.Flags().StringVar(&obsPrefix, "prefix", "", "Object prefix")
	syncObsCmd.Flags().StringVar(&obsFile, "file", "", "Local artifact file path")
	syncObsCmd.Flags().StringVar(&obsKey, "key", "", "Destination logical path")
	syncObsCmd.Flags().StringVar(&obsAK, "ak", "", "Access key (or KINSTALL_OBS_AK)")
	syncObsCmd.Flags().StringVar(&obsSK, "sk", "", "Secret key (or KINSTALL_OBS_SK)")
	artCmd.AddCommand(syncObsCmd, &cobra.Command{
		Use:   "verify-sources <policyId>",
		Short: "Probe all configured sources in a MediaPolicy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := deps.Facade.TestMediaPolicy(cmd.Context(), cliActor, projectID, args[0])
			if err != nil {
				return err
			}
			return render(rep, nil, nil)
		},
	})
	root.AddCommand(artCmd)

	// apiserver-proxy (T-M1-20, P1 §3.1)
	var proxyListen, proxyCfg string
	proxyCmd := &cobra.Command{
		Use:   "apiserver-proxy",
		Short: "Run node-local K8s APIServer L4 HA proxy (127.0.0.1:8443, SIGHUP hot-reload)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deps.RunAPIProxy == nil {
				return fmt.Errorf("apiserver-proxy runner not configured")
			}
			return deps.RunAPIProxy(cmd.Context(), proxyListen, proxyCfg)
		},
	}
	proxyCmd.Flags().StringVar(&proxyListen, "listen", "127.0.0.1:8443", "Local TCP listen address")
	proxyCmd.Flags().StringVar(&proxyCfg, "config", "/etc/kinstall/apiserver-proxy/backends.json", "Path to backends.json")
	root.AddCommand(proxyCmd)

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
