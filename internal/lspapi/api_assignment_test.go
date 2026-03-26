package lspapi

import "testing"

func TestApplyAndValidateRGBAssignmentDefaultsToValue(t *testing.T) {
	params := &RGBInvoiceInput{}
	if err := applyAndValidateRGBAssignment(params, "Any"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.Assignment == nil || *params.Assignment != "Any" {
		t.Fatalf("expected assignment Any, got %v", params.Assignment)
	}
}

func TestApplyAndValidateRGBAssignmentNormalizesCase(t *testing.T) {
	in := "value"
	params := &RGBInvoiceInput{Assignment: &in}
	if err := applyAndValidateRGBAssignment(params, "Any"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.Assignment == nil || *params.Assignment != "Any" {
		t.Fatalf("expected normalized assignment Any, got %v", params.Assignment)
	}
}

func TestApplyAndValidateRGBAssignmentRejectsUnsupported(t *testing.T) {
	in := "Other"
	params := &RGBInvoiceInput{Assignment: &in}
	if err := applyAndValidateRGBAssignment(params, "Any"); err == nil {
		t.Fatal("expected error for unsupported assignment")
	}
}

func TestRgbAssignmentJSONAnyValueAlias(t *testing.T) {
	v := "Value"
	got, err := rgbAssignmentJSON(&v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["type"] != "Any" {
		t.Fatalf("expected type Any, got %#v", got)
	}
}
