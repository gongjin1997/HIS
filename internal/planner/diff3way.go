package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// DiffDecision represents the outcome of Day-2 3-Way Diff evaluation (D2 §3.1, ADR-070).
type DiffDecision string

const (
	DiffCreate    DiffDecision = "create"
	DiffUpdate    DiffDecision = "update"
	DiffSkipped   DiffDecision = "skipped"
	DiffConverged DiffDecision = "converged"
	DiffBlocked   DiffDecision = "blocked"
)

// DiffResult summarizes a 3-Way Diff evaluation across lastApplied (base), liveObserved (live), and desired.
type DiffResult struct {
	Decision    DiffDecision `json:"decision"`
	Scenario    int          `json:"scenario"` // 1..6 per D2 §3.1 matrix
	BaseSHA256  string       `json:"baseSha256,omitempty"`
	LiveSHA256  string       `json:"liveSha256,omitempty"`
	DesiredSHA  string       `json:"desiredSha256"`
	UnifiedDiff string       `json:"unifiedDiff,omitempty"`
	Err         error        `json:"-"`
}

// DiffOptions controls override & adoption flags.
type DiffOptions struct {
	ManageMode    sdk.ManageMode
	Adopt         bool // --adopt: accept external drift as new baseline
	ForceOverride bool // --force-override: overwrite remote drift with desired
	AdoptExisting bool // allow taking over an unmanaged pre-existing file (Scenario 6)
}

// Evaluate3Way implements the exact 6-scenario 3-Way Diff matrix in D2 §3.1 (ADR-070).
func Evaluate3Way(path, baseSHA, liveSHA, desiredSHA string, liveExists bool, opts DiffOptions) DiffResult {
	mode := opts.ManageMode
	if mode == "" {
		mode = sdk.ManageModeManaged
	}

	// create-only mode (Sentinel / Patroni self-mutating configs): if remote exists, ALWAYS skip!
	if mode == sdk.ManageModeCreateOnly {
		if liveExists {
			return DiffResult{Decision: DiffSkipped, Scenario: 1, BaseSHA256: baseSHA, LiveSHA256: liveSHA, DesiredSHA: desiredSHA}
		}
		return DiffResult{Decision: DiffCreate, Scenario: 2, DesiredSHA: desiredSHA}
	}

	// Scenario 6: No lastApplied baseline, but file already exists remotely
	if baseSHA == "" {
		if !liveExists {
			return DiffResult{Decision: DiffCreate, Scenario: 2, DesiredSHA: desiredSHA}
		}
		if liveSHA == desiredSHA {
			return DiffResult{Decision: DiffConverged, Scenario: 5, LiveSHA256: liveSHA, DesiredSHA: desiredSHA}
		}
		if opts.AdoptExisting || opts.ForceOverride {
			return DiffResult{Decision: DiffUpdate, Scenario: 6, LiveSHA256: liveSHA, DesiredSHA: desiredSHA}
		}
		return DiffResult{
			Decision:   DiffBlocked,
			Scenario:   6,
			LiveSHA256: liveSHA,
			DesiredSHA: desiredSHA,
			Err: sdk.NewError(sdk.CodeDiffUnmanagedExists, "DIFF", http.StatusConflict, sdk.CodeDiffUnmanagedExists, false,
				fmt.Errorf("unmanaged file already exists at %s (set adoptExisting=true to take over)", path)),
		}
	}

	// File deleted remotely while platform previously managed it -> recreate
	if !liveExists {
		return DiffResult{Decision: DiffCreate, Scenario: 2, BaseSHA256: baseSHA, DesiredSHA: desiredSHA}
	}

	baseEqLive := baseSHA == liveSHA
	baseEqDesired := baseSHA == desiredSHA
	liveEqDesired := liveSHA == desiredSHA

	switch {
	// Scenario 1: base == live == desired -> 100% idempotent skip
	case baseEqLive && baseEqDesired:
		return DiffResult{Decision: DiffSkipped, Scenario: 1, BaseSHA256: baseSHA, LiveSHA256: liveSHA, DesiredSHA: desiredSHA}

	// Scenario 2: base == live != desired -> clean platform update
	case baseEqLive && !baseEqDesired:
		return DiffResult{Decision: DiffUpdate, Scenario: 2, BaseSHA256: baseSHA, LiveSHA256: liveSHA, DesiredSHA: desiredSHA}

	// Scenario 5: base != live && live == desired -> already converged remotely, record baseline without rewrite/restart!
	case !baseEqLive && liveEqDesired:
		return DiffResult{Decision: DiffConverged, Scenario: 5, BaseSHA256: baseSHA, LiveSHA256: liveSHA, DesiredSHA: desiredSHA}

	// Scenario 3: base != live && base == desired -> pure external drift
	case !baseEqLive && baseEqDesired:
		if opts.Adopt {
			return DiffResult{Decision: DiffConverged, Scenario: 3, BaseSHA256: baseSHA, LiveSHA256: liveSHA, DesiredSHA: desiredSHA}
		}
		if opts.ForceOverride {
			return DiffResult{Decision: DiffUpdate, Scenario: 3, BaseSHA256: baseSHA, LiveSHA256: liveSHA, DesiredSHA: desiredSHA}
		}
		return DiffResult{
			Decision:   DiffBlocked,
			Scenario:   3,
			BaseSHA256: baseSHA,
			LiveSHA256: liveSHA,
			DesiredSHA: desiredSHA,
			Err: sdk.NewError(sdk.CodeDiffExternalDrift, "DIFF", http.StatusConflict, sdk.CodeDiffExternalDrift, false,
				fmt.Errorf("external drift detected on %s: live=%s != base=%s (use --adopt or --force-override)", path, shortHash(liveSHA), shortHash(baseSHA))),
		}

	// Scenario 4: base != live && base != desired && live != desired -> three-way conflict
	default:
		if opts.ForceOverride {
			return DiffResult{Decision: DiffUpdate, Scenario: 4, BaseSHA256: baseSHA, LiveSHA256: liveSHA, DesiredSHA: desiredSHA}
		}
		return DiffResult{
			Decision:   DiffBlocked,
			Scenario:   4,
			BaseSHA256: baseSHA,
			LiveSHA256: liveSHA,
			DesiredSHA: desiredSHA,
			Err: sdk.NewError(sdk.CodeDiffThreeWayConflict, "DIFF", http.StatusConflict, sdk.CodeDiffThreeWayConflict, false,
				fmt.Errorf("3-way conflict on %s: base=%s live=%s desired=%s (use --force-override after review)", path, shortHash(baseSHA), shortHash(liveSHA), shortHash(desiredSHA))),
		}
	}
}

// MergeManagedBlock replaces or appends content inside # BEGIN kinstall:<blockID> ... # END kinstall:<blockID>
// without altering any user-managed lines outside the fence (D2 §3.2).
func MergeManagedBlock(existing, blockID, blockBody string) string {
	beginMarker := fmt.Sprintf("# BEGIN kinstall:%s", blockID)
	endMarker := fmt.Sprintf("# END kinstall:%s", blockID)
	fenced := beginMarker + "\n" + strings.TrimRight(blockBody, "\n") + "\n" + endMarker

	startIdx := strings.Index(existing, beginMarker)
	endIdx := strings.Index(existing, endMarker)
	if startIdx >= 0 && endIdx > startIdx {
		before := existing[:startIdx]
		after := existing[endIdx+len(endMarker):]
		return before + fenced + after
	}
	if existing == "" {
		return fenced + "\n"
	}
	if !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	return existing + fenced + "\n"
}

// SHA256Hex returns lowercase hex SHA-256 of b.
func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
