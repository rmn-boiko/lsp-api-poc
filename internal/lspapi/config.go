package lspapi

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerAddr string

	DatabaseDriver string
	DatabaseURL    string

	LSPBaseURL              string
	LSPToken                string
	RGBNodeBaseURL          string
	RGBNodeToken            string
	HTTPTimeout             time.Duration
	CronEvery               time.Duration
	SendRGBFeeRate          uint64
	MinConfirmations        uint8
	ExpiryMatchToleranceSec uint32
	MinAmtMsat              uint64
	DefaultRGBAssignment    string

	OpenConnectionPath  string
	GetInfoPath         string
	ListConnectionsPath string
	ListChannelsPath    string
	OpenChannelPath     string
	LNInvoicePath       string
	InvoiceStatusPath   string
	CancelLNInvoicePath string
	SendRGBPath         string
	SendLNPath          string

	DecodeLNPath         string
	DecodeRGBPath        string
	RGBInvoicePath       string
	RefreshTransfersPath string
	ListTransfersPath    string
	ListUnspentsPath     string
	CreateUtxosPath      string

	DefaultChannelCapacitySat uint64
	DefaultChannelPushMsat    uint64
	SupportedAssetIDs         []string
	DefaultVirtualOpenMode    string
	UtxoMinCount              uint32
	UtxoTargetCount           uint32
	UtxoSizeSat               uint32
	UtxoFeeRate               uint64
	UtxoSkipSync              bool
}

func LoadConfig() Config {
	cfg := Config{
		ServerAddr:                envOrDefault("SERVER_ADDR", ":8080"),
		DatabaseDriver:            envOrDefault("DATABASE_DRIVER", "sqlite"),
		DatabaseURL:               envOrDefault("DATABASE_URL", "utexo_lsp.db"),
		LSPBaseURL:                strings.TrimRight(envOrDefault("LSP_BASE_URL", "http://127.0.0.1:3001"), "/"),
		LSPToken:                  os.Getenv("LSP_TOKEN"),
		RGBNodeBaseURL:            strings.TrimRight(envOrDefault("RGB_NODE_BASE_URL", envOrDefault("LSP_BASE_URL", "http://127.0.0.1:3001")), "/"),
		RGBNodeToken:              os.Getenv("RGB_NODE_TOKEN"),
		HTTPTimeout:               durationOrDefault("HTTP_TIMEOUT", 15*time.Second),
		CronEvery:                 durationOrDefault("CRON_EVERY", 30*time.Second),
		SendRGBFeeRate:            uint64(intOrDefault("SENDRGB_FEE_RATE", 1)),
		MinConfirmations:          uint8(intOrDefault("MIN_CONFIRMATIONS", 1)),
		ExpiryMatchToleranceSec:   uint32(intOrDefault("EXPIRY_MATCH_TOLERANCE_SEC", 5)),
		MinAmtMsat:                uint64(intOrDefault("MIN_AMT_MSAT", 3_000_000)),
		DefaultRGBAssignment:      envOrDefault("DEFAULT_RGB_ASSIGNMENT", "Any"),
		OpenConnectionPath:        envOrDefault("LSP_OPENCONNECTION_PATH", "/connectpeer"),
		GetInfoPath:               envOrDefault("LSP_GET_INFO_PATH", "/nodeinfo"),
		ListConnectionsPath:       envOrDefault("LSP_LISTCONNECTIONS_PATH", "/listpeers"),
		ListChannelsPath:          envOrDefault("LSP_LISTCHANNELS_PATH", "/listchannels"),
		OpenChannelPath:           envOrDefault("LSP_OPENCHANNEL_PATH", "/openchannel"),
		LNInvoicePath:             envOrDefault("LSP_LNINVOICE_PATH", "/lninvoice"),
		InvoiceStatusPath:         envOrDefault("LSP_INVOICESTATUS_PATH", "/invoicestatus"),
		CancelLNInvoicePath:       os.Getenv("LSP_CANCELLNINVOICE_PATH"),
		SendRGBPath:               envOrDefault("LSP_SENDRGB_PATH", "/sendrgb"),
		SendLNPath:                envOrDefault("LSP_SENDLN_PATH", "/sendpayment"),
		DecodeLNPath:              envOrDefault("RGB_DECODE_LN_PATH", "/decodelninvoice"),
		DecodeRGBPath:             envOrDefault("RGB_DECODE_RGB_PATH", "/decodergbinvoice"),
		RGBInvoicePath:            envOrDefault("RGB_INVOICE_PATH", "/rgbinvoice"),
		RefreshTransfersPath:      envOrDefault("RGB_REFRESH_TRANSFERS_PATH", "/refreshtransfers"),
		ListTransfersPath:         envOrDefault("RGB_LIST_TRANSFERS_PATH", "/listtransfers"),
		ListUnspentsPath:          envOrDefault("RGB_LIST_UNSPENTS_PATH", "/listunspents"),
		CreateUtxosPath:           envOrDefault("RGB_CREATE_UTXOS_PATH", "/createutxos"),
		DefaultChannelCapacitySat: uint64(intOrDefault("DEFAULT_CHANNEL_CAPACITY_SAT", 200000)),
		DefaultChannelPushMsat:    uint64(intOrDefault("DEFAULT_CHANNEL_PUSH_MSAT", 0)),
		SupportedAssetIDs:         csvOrDefault("SUPPORTED_ASSET_IDS", ""),
		DefaultVirtualOpenMode:    strings.TrimSpace(os.Getenv("DEFAULT_VIRTUAL_OPEN_MODE")),
		UtxoMinCount:              uint32(intOrDefault("UTXO_MIN_COUNT", 0)),
		UtxoTargetCount:           uint32(intOrDefault("UTXO_TARGET_COUNT", 0)),
		UtxoSizeSat:               uint32(intOrDefault("UTXO_SIZE_SAT", 32000)),
		UtxoFeeRate:               uint64(intOrDefault("UTXO_FEE_RATE", 1)),
		UtxoSkipSync:              boolOrDefault("UTXO_SKIP_SYNC", false),
	}

	if cfg.LSPBaseURL == "" {
		log.Fatal("LSP_BASE_URL is required")
	}
	return cfg
}

func envOrDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func intOrDefault(k string, d int) int {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return d
	}
	return i
}

func durationOrDefault(k string, d time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	dur, err := time.ParseDuration(v)
	if err != nil {
		return d
	}
	return dur
}

func csvOrDefault(k, d string) []string {
	v := os.Getenv(k)
	if v == "" {
		v = d
	}
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func boolOrDefault(k string, d bool) bool {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return d
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return d
	}
}
