package lspapi

import "testing"

func TestEnsureAssetSupported(t *testing.T) {
	a := &API{
		cfg: Config{
			SupportedAssetIDs: []string{"assetA", "assetB"},
		},
	}

	if err := a.ensureAssetSupported("assetA"); err != nil {
		t.Fatalf("expected assetA to be supported, got error: %v", err)
	}
	if err := a.ensureAssetSupported("assetX"); err == nil {
		t.Fatal("expected assetX to be rejected")
	}
}

func TestEnsureAssetSupportedRequiresConfig(t *testing.T) {
	a := &API{cfg: Config{}}
	if err := a.ensureAssetSupported("assetA"); err == nil {
		t.Fatal("expected error when SUPPORTED_ASSET_IDS is not configured")
	}
}

func TestIsSupportedAssetAllowsBTC(t *testing.T) {
	a := &API{
		cfg: Config{
			SupportedAssetIDs: []string{"assetA"},
		},
	}

	if !a.isSupportedAsset(nil) {
		t.Fatal("expected nil asset_id (BTC) to be supported")
	}

	empty := "   "
	if !a.isSupportedAsset(&empty) {
		t.Fatal("expected empty asset_id (BTC) to be supported")
	}
}

func TestIsSupportedAssetForRGBRequiresAllowlist(t *testing.T) {
	a := &API{
		cfg: Config{
			SupportedAssetIDs: []string{"assetA"},
		},
	}

	assetA := "assetA"
	if !a.isSupportedAsset(&assetA) {
		t.Fatal("expected supported RGB asset to pass")
	}

	assetB := "assetB"
	if a.isSupportedAsset(&assetB) {
		t.Fatal("expected unsupported RGB asset to fail")
	}
}
