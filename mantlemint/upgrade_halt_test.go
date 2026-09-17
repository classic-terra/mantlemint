package mantlemint

import (
	"context"
	"errors"
	"testing"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/stretchr/testify/assert"
)

type fakeUpgradeKeeper struct {
	plan     upgradetypes.Plan
	err      error
	handlers map[string]bool
}

func (k fakeUpgradeKeeper) GetUpgradePlan(context.Context) (upgradetypes.Plan, error) {
	return k.plan, k.err
}

func (k fakeUpgradeKeeper) HasHandler(name string) bool {
	return k.handlers[name]
}

func TestIsUpgradeHalt(t *testing.T) {
	plan := upgradetypes.Plan{Name: "v14_3", Height: 100}
	oldBinary := fakeUpgradeKeeper{plan: plan}
	newBinary := fakeUpgradeKeeper{plan: plan, handlers: map[string]bool{"v14_3": true}}

	// old binary: fails at the upgrade height with UPGRADE NEEDED
	assert.False(t, IsUpgradeHalt(context.Background(), oldBinary, 99))
	assert.True(t, IsUpgradeHalt(context.Background(), oldBinary, 100))

	// new binary: fails below the upgrade height with BINARY UPDATED BEFORE TRIGGER
	assert.True(t, IsUpgradeHalt(context.Background(), newBinary, 99))
	assert.False(t, IsUpgradeHalt(context.Background(), newBinary, 100))

	// no plan or unreadable state: an ordinary failure
	noPlan := fakeUpgradeKeeper{err: errors.New("no upgrade plan found")}
	assert.False(t, IsUpgradeHalt(context.Background(), noPlan, 100))
}
