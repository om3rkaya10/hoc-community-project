package accounts

import "testing"

func TestExchangeInscriptionsIsAtomicAndRejectsInsufficientSource(t *testing.T) {
	a := &Account{Inscriptions: map[string]int{"494": 3}}
	if a.ExchangeInscriptions(494, 495, 4) {
		t.Fatal("exchange succeeded with only three source inscriptions")
	}
	if a.Inscriptions["494"] != 3 || a.Inscriptions["495"] != 0 {
		t.Fatalf("failed exchange mutated inventory: %#v", a.Inscriptions)
	}

	a.Inscriptions["494"] = 8
	if !a.ExchangeInscriptions(494, 495, 4) {
		t.Fatal("exchange rejected sufficient source inventory")
	}
	if a.Inscriptions["494"] != 4 || a.Inscriptions["495"] != 1 {
		t.Fatalf("successful exchange inventory=%#v, want 494:4 495:1", a.Inscriptions)
	}
}
