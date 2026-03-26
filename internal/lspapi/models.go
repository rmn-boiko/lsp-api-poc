package lspapi

import (
	"encoding/json"
	"time"
)

type API struct {
	cfg       Config
	db        Store
	lspClient *NodeClient
	rgbClient *NodeClient
}

type OnchainSendRequest struct {
	RGBInvoice string         `json:"rgb_invoice"`
	LNInvoice  LNInvoiceInput `json:"lninvoice"`
}

type LNInvoiceInput struct {
	AmtMsat     *uint64 `json:"amt_msat,omitempty"`
	ExpirySec   uint32  `json:"expiry_sec"`
	AssetID     *string `json:"asset_id,omitempty"`
	AssetAmount *uint64 `json:"asset_amount,omitempty"`
}

type OnchainSendResponse struct {
	RGBInvoice string `json:"rgb_invoice"`
	LNInvoice  string `json:"ln_invoice"`
	MappingID  int64  `json:"mapping_id"`
}

type LightningReceiveRequest struct {
	LNInvoice string          `json:"ln_invoice"`
	RGBParams RGBInvoiceInput `json:"rgb_invoice"`
}

type RGBInvoiceInput struct {
	AssetID          *string `json:"asset_id,omitempty"`
	Assignment       *string `json:"assignment,omitempty"`
	DurationSeconds  *uint32 `json:"duration_seconds,omitempty"`
	MinConfirmations uint8   `json:"min_confirmations"`
	Witness          bool    `json:"witness"`
}

type LightningReceiveResponse struct {
	LNInvoice  string `json:"ln_invoice"`
	RGBInvoice string `json:"rgb_invoice"`
	MappingID  int64  `json:"mapping_id"`
}

type decodeLNResponse struct {
	AmtMsat     *uint64 `json:"amt_msat"`
	ExpirySec   uint64  `json:"expiry_sec"`
	Timestamp   uint64  `json:"timestamp"`
	AssetID     *string `json:"asset_id"`
	AssetAmount *uint64 `json:"asset_amount"`
}

type decodeRGBResponse struct {
	RecipientID         string   `json:"recipient_id"`
	Assignment          any      `json:"assignment"`
	AssetID             *string  `json:"asset_id"`
	ExpirationTimestamp *int64   `json:"expiration_timestamp"`
	TransportEndpoints  []string `json:"transport_endpoints"`
}

type invoiceStatusResponse struct {
	Status string `json:"status"`
}

type rgbInvoiceResponse struct {
	Invoice             string `json:"invoice"`
	ExpirationTimestamp *int64 `json:"expiration_timestamp"`
	BatchTransferIdx    int64  `json:"batch_transfer_idx"`
}

type listConnectionsResponse struct {
	Connections []Connection `json:"connections"`
}

type Connection struct {
	PeerPubkeyAndOptAddr string          `json:"peer_pubkey_and_opt_addr"`
	CapacitySat          uint64          `json:"capacity_sat"`
	PushMsat             uint64          `json:"push_msat"`
	AssetID              *string         `json:"asset_id"`
	Public               bool            `json:"public"`
	WithAnchors          bool            `json:"with_anchors"`
	AssetDecimals        *uint8          `json:"asset_decimals"`
	OpenChannelParams    json.RawMessage `json:"openchannel_params"`
}

type listChannelsResponse struct {
	Channels []Channel `json:"channels"`
}

type listPeersResponse struct {
	Peers []Peer `json:"peers"`
}

type Peer struct {
	Pubkey string `json:"pubkey"`
}

type listTransfersRequest struct {
	AssetID string `json:"asset_id"`
}

type listTransfersResponse struct {
	Transfers []Transfer `json:"transfers"`
}

type listUnspentsRequest struct {
	SkipSync bool `json:"skip_sync"`
}

type listUnspentsResponse struct {
	Unspents []Unspent `json:"unspents"`
}

type Unspent struct {
	Utxo Utxo `json:"utxo"`
}

type Utxo struct {
	Outpoint  string `json:"outpoint"`
	BtcAmount uint64 `json:"btc_amount"`
	Colorable bool   `json:"colorable"`
}

type createUtxosRequest struct {
	UpTo     bool    `json:"up_to"`
	Num      *uint8  `json:"num,omitempty"`
	Size     *uint32 `json:"size,omitempty"`
	FeeRate  uint64  `json:"fee_rate"`
	SkipSync bool    `json:"skip_sync"`
}

type Transfer struct {
	Idx    int64  `json:"idx"`
	Status string `json:"status"`
}

type Channel struct {
	PeerPubkey string  `json:"peer_pubkey"`
	AssetID    *string `json:"asset_id"`
}

type OpenChannelRequest struct {
	PeerPubkeyAndOptAddr      string  `json:"peer_pubkey_and_opt_addr"`
	CapacitySat               uint64  `json:"capacity_sat"`
	PushMsat                  uint64  `json:"push_msat"`
	AssetID                   *string `json:"asset_id,omitempty"`
	PushAssetAmount           *uint64 `json:"push_asset_amount,omitempty"`
	Public                    bool    `json:"public"`
	WithAnchors               bool    `json:"with_anchors"`
	FeeBaseMsat               *uint32 `json:"fee_base_msat,omitempty"`
	FeeProportionalMillionths *uint32 `json:"fee_proportional_millionths,omitempty"`
	TemporaryChannelID        *string `json:"temporary_channel_id,omitempty"`
	VirtualOpenMode           *string `json:"virtual_open_mode,omitempty"`
}

type OnchainSendRecord struct {
	ID             int64
	UserRGBInvoice string
	LspLNInvoice   string
	Status         string
	LNExpiresAt    *time.Time
	CreatedAt      time.Time
}

type LightningReceiveRecord struct {
	ID               int64
	UserLNInvoice    string
	LspRGBInvoice    string
	RGBAssetID       string
	BatchTransferIdx int64
	Status           string
	RGBExpiresAt     *time.Time
	CreatedAt        time.Time
}
