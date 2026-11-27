package engine

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// CheckResult indicates whether a Task is already converged (Skipped) or requires Apply.
type CheckResult struct {
	Converged bool
	Summary   string
	Diff      map[string]any
}

// ApplyResult carries rollback anchors and metadata returned by Task.Apply.
type ApplyResult struct {
	Changed   bool
	BackupRef map[string]any
	Output    string
}

// RunContext supplies all platform capabilities to a built-in Task (05 §3.3).
type RunContext struct {
	OpID               string
	TaskRunID          string
	PhaseName          string
	Host               sdk.HostView
	Session            ports.Session
	OS                 ports.OSAdapter
	Render             ports.Renderer
	Eval               ports.Evaluator
	Secrets            ports.SecretStore
	Media              ports.MediaResolver
	MediaPolicy        domain.MediaPolicy
	PKI                ports.PKIIssuer
	Logger             *slog.Logger
	LogStdout          io.Writer // optional RedactWriter-wrapped live stdout SSE stream
	LogStderr          io.Writer // optional RedactWriter-wrapped live stderr SSE stream
	Notify             func(handler string)
	RegisterSecret     func(raw string)
	LastApplied        map[string]string // path -> lastApplied SHA-256 for 3-Way Diff
	LastAppliedContent map[string]string // path -> scrubbed content snapshot persisted to Instance.StateJSON
	Adopt              bool
	ForceOverride      bool
	DryRun             bool
}

// Task is the Command + Template Method interface implemented by all 15 built-in tasks (D1 §1.2 #3, D2 §5.1).
type Task interface {
	Name() string
	Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error)
	Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error)
	Rollback(ctx context.Context, rc *RunContext, spec sdk.TaskSpec, prev ApplyResult) error
}

// TaskRegistry stores the 15 built-in platform Tasks, frozen at startup (zero init()).
type TaskRegistry struct {
	tasks  map[string]Task
	frozen bool
}

// NewDefaultTaskRegistry constructs and freezes the canonical 15 built-in platform Tasks (D2 §5.1).
func NewDefaultTaskRegistry() *TaskRegistry {
	reg := &TaskRegistry{tasks: make(map[string]Task, 15)}
	all := []Task{
		&FileSyncTask{},
		&TemplateRenderTask{},
		&PkgEnsureTask{},
		&BinaryInstallTask{},
		&SystemdUnitTask{},
		&SysctlApplyTask{},
		&KmodEnsureTask{},
		&CmdRunTask{},
		&WaitTCPTask{},
		&WaitHTTPTask{},
		&CertIssueTask{},
		&HelmReleaseTask{},
		&DockerEnsureTask{},
		&ImageLoadTask{},
		&ComposeSyncTask{},
	}
	for _, t := range all {
		reg.tasks[t.Name()] = t
	}
	reg.frozen = true
	return reg
}

// Get retrieves a registered Task by canonical name.
func (r *TaskRegistry) Get(name string) (Task, error) {
	t, ok := r.tasks[name]
	if !ok {
		return nil, sdk.NewError(sdk.CodeEngUnknownTask, "ENG", http.StatusUnprocessableEntity, sdk.CodeEngUnknownTask, false,
			fmt.Errorf("unknown built-in task type %q", name))
	}
	return t, nil
}

// Names returns all registered built-in task names.
func (r *TaskRegistry) Names() []string {
	out := make([]string, 0, len(r.tasks))
	for k := range r.tasks {
		out = append(out, k)
	}
	return out
}
