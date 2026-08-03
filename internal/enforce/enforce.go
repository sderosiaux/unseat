package enforce

import (
	"context"
	"fmt"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/sderosiaux/unseat/internal/store"
)

type Candidate struct {
	Decision   core.Decision `json:"decision"`
	Executable bool          `json:"executable"`
	BlockedBy  []string      `json:"blocked_by,omitempty"`
}

type Engine struct {
	store    store.Store
	registry *provider.Registry
}

func New(s store.Store, registry *provider.Registry) *Engine {
	return &Engine{store: s, registry: registry}
}

func (e *Engine) Plan(ctx context.Context, filter store.DecisionFilter) ([]Candidate, error) {
	status := core.DecisionApproved
	filter.Status = &status
	decisions, err := e.store.ListDecisions(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(decisions))
	for _, d := range decisions {
		out = append(out, e.candidate(ctx, d))
	}
	return out, nil
}

func (e *Engine) Apply(ctx context.Context, decisionID, actor string) (*core.Decision, error) {
	decision, err := e.store.GetDecision(ctx, decisionID)
	if err != nil {
		return nil, err
	}
	if decision == nil {
		return nil, fmt.Errorf("decision %q not found", decisionID)
	}
	candidate := e.candidate(ctx, *decision)
	if !candidate.Executable {
		return nil, fmt.Errorf("decision %q is not executable: %v", decisionID, candidate.BlockedBy)
	}

	p, err := e.registry.Get(decision.Provider)
	if err != nil {
		return nil, err
	}
	switch decision.ActionClass {
	case core.ActionRemoveWorkspaceMember:
		if err := p.RemoveUser(ctx, decision.ObjectID); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("decision %q action %s is not implemented by enforce", decisionID, decision.ActionClass)
	}

	return e.store.MarkDecisionExecuted(ctx, decisionID, actor)
}

func (e *Engine) candidate(_ context.Context, d core.Decision) Candidate {
	c := Candidate{Decision: d}
	if d.Status != core.DecisionApproved {
		c.BlockedBy = append(c.BlockedBy, "decision_not_approved")
		return c
	}
	if d.ActionClass != core.ActionRemoveWorkspaceMember {
		c.BlockedBy = append(c.BlockedBy, "action_class_not_implemented_by_enforce")
		return c
	}
	p, err := e.registry.Get(d.Provider)
	if err != nil {
		c.BlockedBy = append(c.BlockedBy, "provider_not_registered")
		return c
	}
	if !core.SupportsAction(p.Capabilities().ActionMatrix(d.Provider), d.ActionClass) {
		c.BlockedBy = append(c.BlockedBy, "provider_does_not_support_action_class")
		return c
	}
	c.Executable = true
	return c
}
