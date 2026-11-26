package supportbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/obs"
	"github.com/kinstall/kinstall/internal/ports"
)

// BundleReport describes the redacted support-bundle archive (T-M2-05, Redline 6).
type BundleReport struct {
	GeneratedAt    time.Time `json:"generatedAt"`
	ProjectID      string    `json:"projectId"`
	HostCount      int       `json:"hostCount"`
	InstanceCount  int       `json:"instanceCount"`
	OperationCount int       `json:"operationCount"`
	AuditEntries   int       `json:"auditEntries"`
	SizeBytes      int       `json:"sizeBytes"`
}

// Collect gathers diagnostic metadata (hosts, instances, operations, audit summary, extra logs),
// passes EVERY SINGLE BYTE through obs.RedactWriter to guarantee zero secret leakage, and returns a .tar.gz archive.
func Collect(ctx context.Context, st ports.Store, projectID string, extraLogs string, extraSecrets []string) ([]byte, *BundleReport, error) {
	hosts, _ := st.Hosts().ListByProject(ctx, projectID)
	insts, _ := st.Instances().ListByProject(ctx, projectID)
	ops, _ := st.Operations().ListByProject(ctx, projectID, 100)
	audits, _ := st.Audit().List(ctx, projectID, 0, 200)

	redact := func(raw []byte) []byte {
		var out bytes.Buffer
		rw := obs.NewRedactWriter(&out, extraSecrets...)
		_, _ = rw.Write(raw)
		_ = rw.Close()
		return out.Bytes()
	}

	hostsJSON, _ := json.MarshalIndent(hosts, "", "  ")
	instsJSON, _ := json.MarshalIndent(insts, "", "  ")
	opsJSON, _ := json.MarshalIndent(ops, "", "  ")
	auditJSON, _ := json.MarshalIndent(audits, "", "  ")

	files := map[string][]byte{
		"hosts.json":      redact(hostsJSON),
		"instances.json":  redact(instsJSON),
		"operations.json": redact(opsJSON),
		"audit-tail.json": redact(auditJSON),
		"runtime.log":     redact([]byte(extraLogs)),
	}

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	now := time.Now().UTC()
	for name, data := range files {
		hdr := &tar.Header{Name: "support-bundle/" + name, Mode: 0o640, Size: int64(len(data)), ModTime: now}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, nil, err
	}
	raw := buf.Bytes()
	return raw, &BundleReport{
		GeneratedAt:    now,
		ProjectID:      projectID,
		HostCount:      len(hosts),
		InstanceCount:  len(insts),
		OperationCount: len(ops),
		AuditEntries:   len(audits),
		SizeBytes:      len(raw),
	}, nil
}

// RenderOperationReport produces a Markdown or JSON report for an Operation and its TaskRuns (T-M2-05).
func RenderOperationReport(op *domain.Operation, runs []domain.TaskRun, format string) ([]byte, error) {
	if strings.EqualFold(format, "json") {
		return json.MarshalIndent(map[string]any{"operation": op, "taskRuns": runs}, "", "  ")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Operation Report: `%s`\n\n", op.ID)
	fmt.Fprintf(&b, "- **Target**: `%s/%s`\n", op.TargetType, op.TargetID)
	fmt.Fprintf(&b, "- **Action**: `%s`\n", op.Action)
	fmt.Fprintf(&b, "- **State**: `%s` (Fencing #%d)\n", op.State, op.FencingToken)
	if op.ErrorCode != nil && *op.ErrorCode != "" {
		msg := ""
		if op.ErrorMessage != nil {
			msg = *op.ErrorMessage
		}
		fmt.Fprintf(&b, "- **Error**: `%s` — %s\n", *op.ErrorCode, msg)
	}
	b.WriteString("\n## Task Runs\n\n| Phase | Step | Host | State | Duration (ms) |\n| :---: | --- | --- | :---: | ---: |\n")
	for _, r := range runs {
		hid := "-"
		if r.HostID != nil {
			hid = *r.HostID
		}
		var durMs int64
		if r.StartedAt != nil && r.FinishedAt != nil {
			durMs = r.FinishedAt.Sub(*r.StartedAt).Milliseconds()
		}
		fmt.Fprintf(&b, "| %d | `%s` | `%s` | `%s` | %d |\n", r.PhaseIdx, r.StepKey, hid, r.State, durMs)
	}
	return []byte(b.String()), nil
}
