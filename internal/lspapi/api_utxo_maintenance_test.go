package lspapi

import "testing"

func TestUtxoMaintenanceDecisionDisabled(t *testing.T) {
	create, num, err := utxoMaintenanceDecision(0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if create || num != 0 {
		t.Fatalf("expected disabled decision, got create=%v num=%d", create, num)
	}
}

func TestUtxoMaintenanceDecisionValid(t *testing.T) {
	create, num, err := utxoMaintenanceDecision(3, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !create || num != 7 {
		t.Fatalf("expected create=true num=7, got create=%v num=%d", create, num)
	}
}

func TestUtxoMaintenanceDecisionInvalidRange(t *testing.T) {
	if _, _, err := utxoMaintenanceDecision(5, 5); err == nil {
		t.Fatal("expected error for target<=min")
	}
}
