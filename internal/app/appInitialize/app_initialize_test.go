package appInitialize

import "testing"

func TestModuleLoadOrderProvidesCrossModuleContracts(t *testing.T) {
	positions := map[string]int{}
	for i, module := range GetApps() {
		positions[module.Info()] = i
	}

	assertBefore(t, positions, "order", "payment")
	assertBefore(t, positions, "order", "item")
	assertBefore(t, positions, "deposit", "item")
	assertBefore(t, positions, "item", "room")
}

func assertBefore(t *testing.T, positions map[string]int, before, after string) {
	t.Helper()
	beforeIndex, ok := positions[before]
	if !ok {
		t.Fatalf("missing module %q", before)
	}
	afterIndex, ok := positions[after]
	if !ok {
		t.Fatalf("missing module %q", after)
	}
	if beforeIndex >= afterIndex {
		t.Fatalf("expected %s to load before %s, got positions %d >= %d", before, after, beforeIndex, afterIndex)
	}
}
