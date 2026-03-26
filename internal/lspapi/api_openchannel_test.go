package lspapi

import (
	"encoding/json"
	"testing"
)

func TestOpenChannelPayloadAddsDefaultVirtualModeToRequest(t *testing.T) {
	mode := "outbound"
	a := &API{
		cfg: Config{
			DefaultVirtualOpenMode: mode,
		},
	}

	payload, err := a.openChannelPayload(Connection{
		PeerPubkeyAndOptAddr: "02abc@127.0.0.1:9735",
		CapacitySat:          200000,
		PushMsat:             0,
		Public:               false,
		WithAnchors:          true,
	})
	if err != nil {
		t.Fatalf("openChannelPayload failed: %v", err)
	}

	req, ok := payload.(OpenChannelRequest)
	if !ok {
		t.Fatalf("expected OpenChannelRequest, got %T", payload)
	}
	if req.VirtualOpenMode == nil || *req.VirtualOpenMode != mode {
		t.Fatalf("expected virtual mode %q, got %v", mode, req.VirtualOpenMode)
	}
}

func TestOpenChannelPayloadInjectsDefaultVirtualModeIntoMapPayload(t *testing.T) {
	mode := "outbound"
	a := &API{
		cfg: Config{
			DefaultVirtualOpenMode: mode,
		},
	}

	params := map[string]any{
		"peer_pubkey_and_opt_addr": "02abc@127.0.0.1:9735",
		"capacity_sat":             200000,
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	payload, err := a.openChannelPayload(Connection{OpenChannelParams: raw})
	if err != nil {
		t.Fatalf("openChannelPayload failed: %v", err)
	}

	m, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", payload)
	}
	if got, ok := m["virtual_open_mode"].(string); !ok || got != mode {
		t.Fatalf("expected virtual_open_mode=%q, got %#v", mode, m["virtual_open_mode"])
	}
}

func TestOpenChannelPayloadPreservesExplicitVirtualModeInMapPayload(t *testing.T) {
	a := &API{
		cfg: Config{
			DefaultVirtualOpenMode: "outbound",
		},
	}

	params := map[string]any{
		"peer_pubkey_and_opt_addr": "02abc@127.0.0.1:9735",
		"capacity_sat":             200000,
		"virtual_open_mode":        "inbound",
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	payload, err := a.openChannelPayload(Connection{OpenChannelParams: raw})
	if err != nil {
		t.Fatalf("openChannelPayload failed: %v", err)
	}

	m, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", payload)
	}
	if got, ok := m["virtual_open_mode"].(string); !ok || got != "inbound" {
		t.Fatalf("expected explicit virtual_open_mode to remain \"inbound\", got %#v", m["virtual_open_mode"])
	}
}
