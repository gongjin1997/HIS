package lock

import (
	"context"
	"fmt"
	"time"

	"github.com/kinstall/kinstall/internal/ports"
)

// MaintenanceWindow defines allowed execution windows for disruptive operations (D2 §2.3).
type MaintenanceWindow struct {
	Enabled   bool           `json:"enabled"`
	Weekdays  []time.Weekday `json:"weekdays"`  // empty means every day
	StartHour int            `json:"startHour"` // 0-23
	EndHour   int            `json:"endHour"`   // 0-23 (if < StartHour wraps past midnight)
	Timezone  string         `json:"timezone"`  // e.g. "Asia/Shanghai"
}

// Allows reports whether now falls inside the maintenance window.
func (w MaintenanceWindow) Allows(now time.Time) bool {
	if !w.Enabled {
		return true
	}
	loc := time.UTC
	if w.Timezone != "" {
		if l, err := time.LoadLocation(w.Timezone); err == nil {
			loc = l
		}
	}
	local := now.In(loc)
	if len(w.Weekdays) > 0 {
		okDay := false
		for _, wd := range w.Weekdays {
			if local.Weekday() == wd {
				okDay = true
				break
			}
		}
		if !okDay {
			return false
		}
	}
	hr := local.Hour()
	if w.StartHour <= w.EndHour {
		return hr >= w.StartHour && hr < w.EndHour
	}
	return hr >= w.StartHour || hr < w.EndHour
}

// Manager implements ports.Locker over ports.LockRepo with Stack -> Instance -> Host sorted batch acquisition (D2 §2.3).
type Manager struct {
	repo ports.LockRepo
}

func NewManager(repo ports.LockRepo) *Manager {
	return &Manager{repo: repo}
}

func (m *Manager) AcquireAll(ctx context.Context, opID string, fencing int64, keys []ports.LockKey, ttl time.Duration) (func(context.Context) error, error) {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	if err := m.repo.AcquireBatch(ctx, opID, fencing, keys, ttl); err != nil {
		return nil, err
	}
	release := func(relCtx context.Context) error {
		if relCtx == nil {
			relCtx = context.Background()
		}
		return m.repo.ReleaseByOp(relCtx, opID)
	}
	return release, nil
}

func (m *Manager) Renew(ctx context.Context, opID string, fencing int64, ttl time.Duration) error {
	return m.repo.RenewByOp(ctx, opID, fencing, ttl)
}

// CheckWindow validates maintenance window admission for disruptive operations.
func CheckWindow(w MaintenanceWindow, now time.Time, emergencyOverride bool) error {
	if emergencyOverride || w.Allows(now) {
		return nil
	}
	return fmt.Errorf("disruptive operation deferred: outside maintenance window (%02d:00-%02d:00 %s)", w.StartHour, w.EndHour, w.Timezone)
}
