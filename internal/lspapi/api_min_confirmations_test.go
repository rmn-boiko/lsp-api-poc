package lspapi

import "testing"

func TestApplyBackendMinConfirmationsAlwaysOverridesInput(t *testing.T) {
	params := &RGBInvoiceInput{MinConfirmations: 9}
	applyBackendMinConfirmations(params, 1)
	if params.MinConfirmations != 1 {
		t.Fatalf("expected backend min_confirmations=1, got %d", params.MinConfirmations)
	}
}

func TestApplyBackendMinConfirmationsNilSafe(t *testing.T) {
	applyBackendMinConfirmations(nil, 1)
}
