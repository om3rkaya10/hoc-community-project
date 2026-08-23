package accounts

import "testing"

func TestTalentGroupsExposeSharedRemainingBudgetForAllFourClasses(t *testing.T) {
	a := &Account{TalentPoints: 40}
	groups := a.EnsureTalentPages()
	for id, g := range groups {
		budgets := []int{g.F14, g.F18, g.Limit, g.F20}
		for classIndex, got := range budgets {
			if got != 40 {
				t.Fatalf("group=%d class=%d budget=%d, want 40", id, classIndex, got)
			}
		}
	}

	_, g := a.ApplyTalentUpdate(1, [][]int{{101, 40, 0}}, false)
	if g.Echo != 0 {
		t.Fatalf("remaining=%d, want 0", g.Echo)
	}
	// Class caps stay available so the four branches remain browsable; only the
	// shared remaining balance is exhausted.
	for classIndex, got := range []int{g.F14, g.F18, g.Limit, g.F20} {
		if got != 40 {
			t.Fatalf("after spend class=%d cap=%d, want 40", classIndex, got)
		}
	}
}

func TestTalentUpdateClampsTotalRanksToAccountBudget(t *testing.T) {
	a := &Account{TalentPoints: 40}
	_, g := a.ApplyTalentUpdate(1, [][]int{{101, 30, 0}, {102, 16, 0}}, false)
	if spent := talentSpent(g.Talents); spent != 40 {
		t.Fatalf("persisted spent=%d, want 40", spent)
	}
	if g.Echo != 0 {
		t.Fatalf("remaining=%d, want 0", g.Echo)
	}
}
