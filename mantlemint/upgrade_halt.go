package mantlemint

import (
	"context"

	upgradetypes "cosmossdk.io/x/upgrade/types"
)

// UpgradePlanReader is the part of the upgrade keeper IsUpgradeHalt needs.
type UpgradePlanReader interface {
	GetUpgradePlan(ctx context.Context) (upgradetypes.Plan, error)
	HasHandler(name string) bool
}

// IsUpgradeHalt reports whether the upgrade module refuses a block at height,
// using the same two conditions as its PreBlocker: the plan is due but this
// binary has no handler for it, or the binary has the handler before the plan
// is due. ctx must read the state the block would be applied on. Any error,
// including the absence of a plan, counts as no halt.
func IsUpgradeHalt(ctx context.Context, keeper UpgradePlanReader, height int64) bool {
	plan, err := keeper.GetUpgradePlan(ctx)
	if err != nil {
		return false
	}
	return plan.ShouldExecute(height) != keeper.HasHandler(plan.Name)
}
