package engine

import (
	"context"
	"errors"
	"time"

	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/selfprotect"
)

// The repair rate limit. After RepairLimit repairs of one asset, failed or
// not, within RepairWindow, Gryph stops repairing it and records a tamper
// event. A reconcile pass that fights an agent over a file must not loop.
const (
	RepairLimit  = 3
	RepairWindow = time.Hour
)

// RepairFailure is one asset that a repair did not restore.
type RepairFailure struct {
	Status selfprotect.AssetStatus
	Err    error
}

// ReconcileReport is the outcome of one reconcile pass.
type ReconcileReport struct {
	// Statuses is the state of every asset after the pass.
	Statuses []selfprotect.AssetStatus
	Profile  selfprotect.Profile
	// Recorded holds the tamper events the pass wrote.
	Recorded []*events.Event
	// Repaired holds the assets the pass restored.
	Repaired []selfprotect.AssetStatus
	// Failed holds the assets the pass tried to repair and could not.
	Failed []RepairFailure
	// RateLimited holds the assets the pass left alone, because their
	// repairs in the window reached the limit.
	RateLimited []selfprotect.AssetStatus
}

// RepairEnabled reports whether the configuration lets a reconcile pass
// repair hook configurations.
func RepairEnabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Policy.SelfProtection.Repair
}

// Reconcile runs one pass: it assesses every asset, records the changes
// since the last pass as tamper events, and, when repair is true, repairs
// the hook configurations with drift and records each outcome. It returns
// the report it has so far with the error, so the caller can still show
// the state.
func (a *Runtime) Reconcile(ctx context.Context, repair bool) (*ReconcileReport, error) {
	provider := a.ProtectionProvider()
	report := &ReconcileReport{Statuses: provider.Assess(ctx)}
	report.Profile = selfprotect.ProfileOf(report.Statuses)

	recorder, err := a.TamperRecorder()
	if err != nil {
		return report, err
	}
	history, err := recorder.loadHistory(ctx)
	if err != nil {
		return report, err
	}
	report.Recorded, err = recorder.recordChanges(ctx, history, report.Statuses)
	if err != nil {
		return report, err
	}
	if !repair {
		return report, nil
	}

	var only []selfprotect.AssetRef
	for _, s := range report.Statuses {
		if s.Asset != selfprotect.AssetHookConfig || s.Drift == "" {
			continue
		}
		if history.repairAttempts(s.Ref(), time.Now().Add(-RepairWindow)) >= RepairLimit {
			report.RateLimited = append(report.RateLimited, s)
			if latest := history.latest[tamperKey(s.Ref())]; latest.Operation != events.TamperRateLimited {
				payload := newTamperPayload(s)
				payload.Operation = events.TamperRateLimited
				payload.LevelBefore = latest.LevelAfter
				event, err := recorder.record(ctx, history, payload)
				if err != nil {
					return report, err
				}
				report.Recorded = append(report.Recorded, event)
			}
			continue
		}
		only = append(only, s.Ref())
	}
	if len(only) == 0 {
		return report, nil
	}

	after, repairErr := provider.Repair(ctx, selfprotect.RepairOptions{Only: only})
	for _, s := range after {
		payload := newTamperPayload(s)
		payload.LevelBefore = history.latest[tamperKey(s.Ref())].LevelAfter
		if failure := repairErrorFor(repairErr, s.Ref()); failure != nil {
			payload.Operation = events.TamperRepairFailed
			payload.Error = failure.Error()
			report.Failed = append(report.Failed, RepairFailure{Status: s, Err: failure})
		} else {
			payload.Operation = events.TamperRepair
			report.Repaired = append(report.Repaired, s)
		}
		event, err := recorder.record(ctx, history, payload)
		if err != nil {
			return report, err
		}
		report.Recorded = append(report.Recorded, event)
		replaceStatus(report.Statuses, s)
	}
	report.Profile = selfprotect.ProfileOf(report.Statuses)
	return report, nil
}

// repairErrorFor returns the error of ref inside the joined error of a
// Repair call, or nil when ref was repaired.
func repairErrorFor(joined error, ref selfprotect.AssetRef) error {
	if joined == nil {
		return nil
	}
	errs := []error{joined}
	if multi, ok := joined.(interface{ Unwrap() []error }); ok {
		errs = multi.Unwrap()
	}
	for _, err := range errs {
		var repairErr *selfprotect.RepairError
		if errors.As(err, &repairErr) && repairErr.Ref == ref {
			return repairErr.Err
		}
	}
	return nil
}

func replaceStatus(statuses []selfprotect.AssetStatus, s selfprotect.AssetStatus) {
	for i := range statuses {
		if statuses[i].Ref() == s.Ref() {
			statuses[i] = s
			return
		}
	}
}
