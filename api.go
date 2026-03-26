package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

func NewAPI(cfg Config, db Store) *API {
	return &API{
		cfg:       cfg,
		db:        db,
		lspClient: NewNodeClient(cfg.LSPBaseURL, cfg.LSPToken, int64(cfg.HTTPTimeout/time.Second)),
		rgbClient: NewNodeClient(cfg.RGBNodeBaseURL, cfg.RGBNodeToken, int64(cfg.HTTPTimeout/time.Second)),
	}
}

func (a *API) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("GET /get_info", a.handleGetInfo)
	mux.HandleFunc("POST /onchain_send", a.handleOnchainSend)
	mux.HandleFunc("POST /lightning_receive", a.handleLightningReceive)
	return mux
}

func (a *API) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) handleGetInfo(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.HTTPTimeout)
	defer cancel()

	var raw json.RawMessage
	if err := a.getOrPost(ctx, a.lspClient, a.cfg.GetInfoPath, &raw); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}

	writeRawJSON(w, http.StatusOK, raw)
}

func (a *API) handleOnchainSend(w http.ResponseWriter, r *http.Request) {
	var req OnchainSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.RGBInvoice) == "" {
		writeErr(w, http.StatusBadRequest, "rgb_invoice is required")
		return
	}
	if req.LNInvoice.ExpirySec == 0 {
		req.LNInvoice.ExpirySec = 3600
	}

	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.HTTPTimeout)
	defer cancel()

	if _, err := a.validateRGBInvoice(ctx, req.RGBInvoice); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	var lnResp struct {
		Invoice string `json:"invoice"`
	}
	if err := a.lspClient.DoJSON(ctx, http.MethodPost, a.cfg.LNInvoicePath, req.LNInvoice, &lnResp); err != nil {
		writeErr(w, http.StatusBadGateway, wrapErr("failed /lninvoice", err).Error())
		return
	}
	if strings.TrimSpace(lnResp.Invoice) == "" {
		writeErr(w, http.StatusBadGateway, "empty lsp lightning invoice")
		return
	}

	lnDecoded, err := a.validateLNInvoice(ctx, lnResp.Invoice)
	if err != nil {
		writeErr(w, http.StatusBadGateway, wrapErr("created ln invoice failed validation", err).Error())
		return
	}

	lnExp := unixFromTimestampAndExpiry(lnDecoded.Timestamp, lnDecoded.ExpirySec)
	id, err := a.db.InsertOnchainSend(ctx, req.RGBInvoice, lnResp.Invoice, &lnExp)
	if err != nil {
		writeErr(w, http.StatusConflict, wrapErr("cannot persist mapping", err).Error())
		return
	}

	writeJSON(w, http.StatusOK, OnchainSendResponse{
		RGBInvoice: req.RGBInvoice,
		LNInvoice:  lnResp.Invoice,
		MappingID:  id,
	})
}

func (a *API) handleLightningReceive(w http.ResponseWriter, r *http.Request) {
	var req LightningReceiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.LNInvoice) == "" {
		writeErr(w, http.StatusBadRequest, "ln_invoice is required")
		return
	}
	if req.RGBParams.AssetID == nil || strings.TrimSpace(*req.RGBParams.AssetID) == "" {
		writeErr(w, http.StatusBadRequest, "rgb_invoice.asset_id is required for transfer monitoring")
		return
	}
	if req.RGBParams.MinConfirmations == 0 {
		req.RGBParams.MinConfirmations = a.cfg.MinConfirmations
	}

	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.HTTPTimeout)
	defer cancel()

	if _, err := a.validateLNInvoice(ctx, req.LNInvoice); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	var rgbResp rgbInvoiceResponse
	if err := a.rgbClient.DoJSON(ctx, http.MethodPost, a.cfg.RGBInvoicePath, req.RGBParams, &rgbResp); err != nil {
		writeErr(w, http.StatusBadGateway, wrapErr("failed /rgbinvoice", err).Error())
		return
	}
	if strings.TrimSpace(rgbResp.Invoice) == "" {
		writeErr(w, http.StatusBadGateway, "empty lsp rgb invoice")
		return
	}
	if rgbResp.BatchTransferIdx == 0 {
		writeErr(w, http.StatusBadGateway, "empty batch_transfer_idx from /rgbinvoice")
		return
	}

	decodedRGB, err := a.validateRGBInvoice(ctx, rgbResp.Invoice)
	if err != nil {
		writeErr(w, http.StatusBadGateway, wrapErr("created rgb invoice failed validation", err).Error())
		return
	}

	var rgbExp *time.Time
	if decodedRGB.ExpirationTimestamp != nil {
		t := time.Unix(*decodedRGB.ExpirationTimestamp, 0).UTC()
		rgbExp = &t
	}

	id, err := a.db.InsertLightningReceive(ctx, req.LNInvoice, rgbResp.Invoice, strings.TrimSpace(*req.RGBParams.AssetID), rgbResp.BatchTransferIdx, rgbExp)
	if err != nil {
		writeErr(w, http.StatusConflict, wrapErr("cannot persist mapping", err).Error())
		return
	}

	writeJSON(w, http.StatusOK, LightningReceiveResponse{
		LNInvoice:  req.LNInvoice,
		RGBInvoice: rgbResp.Invoice,
		MappingID:  id,
	})
}

func (a *API) validateLNInvoice(ctx context.Context, invoice string) (*decodeLNResponse, error) {
	var out decodeLNResponse
	if err := a.rgbClient.DoJSON(ctx, http.MethodPost, a.cfg.DecodeLNPath, map[string]string{"invoice": invoice}, &out); err != nil {
		return nil, wrapErr("/decodelninvoice", err)
	}
	expiresAt := int64(out.Timestamp + out.ExpirySec)
	if time.Now().UTC().Unix() >= expiresAt {
		return nil, errors.New("ln invoice already expired")
	}
	return &out, nil
}

func (a *API) validateRGBInvoice(ctx context.Context, invoice string) (*decodeRGBResponse, error) {
	var out decodeRGBResponse
	if err := a.rgbClient.DoJSON(ctx, http.MethodPost, a.cfg.DecodeRGBPath, map[string]string{"invoice": invoice}, &out); err != nil {
		return nil, wrapErr("/decodergbinvoice", err)
	}
	if out.ExpirationTimestamp != nil && time.Now().UTC().Unix() >= *out.ExpirationTimestamp {
		return nil, errors.New("rgb invoice already expired")
	}
	return &out, nil
}

func (a *API) runCron(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.CronEvery)
	defer ticker.Stop()
	a.runCronTick(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.runCronTick(ctx)
		}
	}
}

func (a *API) runCronTick(ctx context.Context) {
	if err := a.reconcileChannels(ctx); err != nil {
		log.Printf("cron reconcileChannels: %v", err)
	}
	if err := a.monitorOnchainSend(ctx); err != nil {
		log.Printf("cron monitorOnchainSend: %v", err)
	}
	if err := a.monitorLightningReceive(ctx); err != nil {
		log.Printf("cron monitorLightningReceive: %v", err)
	}
}

func (a *API) reconcileChannels(ctx context.Context) error {
	conns, err := a.getConnections(ctx)
	if err != nil {
		return wrapErr("/listconnections", err)
	}

	var chans listChannelsResponse
	if err := a.getOrPost(ctx, a.lspClient, a.cfg.ListChannelsPath, &chans); err != nil {
		return wrapErr("/listchannels", err)
	}

	existing := make(map[string]struct{}, len(chans.Channels))
	for _, c := range chans.Channels {
		existing[channelKey(c.PeerPubkey, c.AssetID)] = struct{}{}
	}

	for _, c := range conns {
		peerKey := peerOnly(c.PeerPubkeyAndOptAddr)
		if _, ok := existing[channelKey(peerKey, c.AssetID)]; ok {
			continue
		}

		payload, err := a.openChannelPayload(c)
		if err != nil {
			log.Printf("skip openchannel payload: %v", err)
			continue
		}
		if err := a.lspClient.DoJSON(ctx, http.MethodPost, a.cfg.OpenChannelPath, payload, nil); err != nil {
			log.Printf("openchannel failed for %s: %v", c.PeerPubkeyAndOptAddr, err)
		}
	}
	return nil
}

func (a *API) monitorOnchainSend(ctx context.Context) error {
	recs, err := a.db.ListOnchainPending(ctx, 200)
	if err != nil {
		return err
	}
	for _, r := range recs {
		if r.LNExpiresAt != nil && time.Now().UTC().After(*r.LNExpiresAt) {
			a.cancelLNInvoice(ctx, r.LspLNInvoice)
			_ = a.db.UpdateOnchainStatus(ctx, r.ID, statusExpired, "ln invoice expired")
			continue
		}

		status, err := a.lnInvoiceStatus(ctx, r.LspLNInvoice)
		if err != nil {
			log.Printf("invoicestatus(%d): %v", r.ID, err)
			continue
		}

		switch normalizeStatus(status) {
		case "succeeded":
			if err := a.sendRGBByInvoice(ctx, r.UserRGBInvoice); err != nil {
				_ = a.db.UpdateOnchainStatus(ctx, r.ID, statusFailed, err.Error())
			} else {
				_ = a.db.UpdateOnchainStatus(ctx, r.ID, statusCompleted, "")
			}
		case "failed":
			_ = a.db.UpdateOnchainStatus(ctx, r.ID, statusFailed, "lsp ln invoice failed")
		case "expired":
			a.cancelLNInvoice(ctx, r.LspLNInvoice)
			_ = a.db.UpdateOnchainStatus(ctx, r.ID, statusExpired, "lsp ln invoice expired")
		}
	}
	return nil
}

func (a *API) monitorLightningReceive(ctx context.Context) error {
	recs, err := a.db.ListLightningPending(ctx, 200)
	if err != nil {
		return err
	}
	if err := a.refreshTransfers(ctx); err != nil {
		return wrapErr("/refreshtransfers", err)
	}
	for _, r := range recs {
		if r.RGBExpiresAt != nil && time.Now().UTC().After(*r.RGBExpiresAt) {
			_ = a.db.UpdateLightningStatus(ctx, r.ID, statusExpired, "rgb invoice expired")
			continue
		}

		status, err := a.transferStatusByIdx(ctx, r.RGBAssetID, r.BatchTransferIdx)
		if err != nil {
			log.Printf("transfer status (%d): %v", r.ID, err)
			continue
		}

		switch normalizeStatus(status) {
		case "succeeded":
			if err := a.sendLNByInvoice(ctx, r.UserLNInvoice); err != nil {
				_ = a.db.UpdateLightningStatus(ctx, r.ID, statusFailed, err.Error())
			} else {
				_ = a.db.UpdateLightningStatus(ctx, r.ID, statusCompleted, "")
			}
		case "settled":
			if err := a.sendLNByInvoice(ctx, r.UserLNInvoice); err != nil {
				_ = a.db.UpdateLightningStatus(ctx, r.ID, statusFailed, err.Error())
			} else {
				_ = a.db.UpdateLightningStatus(ctx, r.ID, statusCompleted, "")
			}
		case "failed":
			_ = a.db.UpdateLightningStatus(ctx, r.ID, statusFailed, "rgb invoice failed")
		case "expired":
			_ = a.db.UpdateLightningStatus(ctx, r.ID, statusExpired, "rgb invoice expired")
		}
	}
	return nil
}

func (a *API) lnInvoiceStatus(ctx context.Context, invoice string) (string, error) {
	var out invoiceStatusResponse
	err := a.lspClient.DoJSON(ctx, http.MethodPost, a.cfg.InvoiceStatusPath, map[string]string{"invoice": invoice}, &out)
	if err != nil {
		return "", err
	}
	return out.Status, nil
}

func (a *API) sendRGBByInvoice(ctx context.Context, rgbInvoice string) error {
	decoded, err := a.validateRGBInvoice(ctx, rgbInvoice)
	if err != nil {
		return err
	}
	if decoded.AssetID == nil || *decoded.AssetID == "" {
		return errors.New("rgb invoice has no asset_id")
	}

	type recipient struct {
		RecipientID        string   `json:"recipient_id"`
		Assignment         any      `json:"assignment"`
		TransportEndpoints []string `json:"transport_endpoints"`
	}
	payload := map[string]any{
		"donation":          false,
		"fee_rate":          a.cfg.SendRGBFeeRate,
		"min_confirmations": a.cfg.MinConfirmations,
		"skip_sync":         false,
		"recipient_map": map[string][]recipient{
			*decoded.AssetID: {
				{
					RecipientID:        decoded.RecipientID,
					Assignment:         decoded.Assignment,
					TransportEndpoints: decoded.TransportEndpoints,
				},
			},
		},
	}
	return a.lspClient.DoJSON(ctx, http.MethodPost, a.cfg.SendRGBPath, payload, nil)
}

func (a *API) sendLNByInvoice(ctx context.Context, lnInvoice string) error {
	payload := map[string]any{"invoice": lnInvoice}
	err := a.lspClient.DoJSON(ctx, http.MethodPost, a.cfg.SendLNPath, payload, nil)
	if err == nil {
		return nil
	}
	if a.cfg.SendLNPath == "/sendln" {
		if hErr, ok := err.(*HTTPError); ok && hErr.StatusCode == http.StatusNotFound {
			return a.lspClient.DoJSON(ctx, http.MethodPost, "/sendpayment", payload, nil)
		}
	}
	return err
}

func (a *API) refreshTransfers(ctx context.Context) error {
	return a.rgbClient.DoJSON(ctx, http.MethodPost, a.cfg.RefreshTransfersPath, map[string]any{"skip_sync": false}, nil)
}

func (a *API) transferStatusByIdx(ctx context.Context, assetID string, batchTransferIdx int64) (string, error) {
	var out listTransfersResponse
	err := a.rgbClient.DoJSON(ctx, http.MethodPost, a.cfg.ListTransfersPath, listTransfersRequest{AssetID: assetID}, &out)
	if err != nil {
		return "", err
	}
	for _, t := range out.Transfers {
		if t.Idx == batchTransferIdx {
			return t.Status, nil
		}
	}
	return "", fmt.Errorf("transfer idx %d not found for asset %s", batchTransferIdx, assetID)
}

func (a *API) getConnections(ctx context.Context) ([]Connection, error) {
	var raw json.RawMessage
	if err := a.getOrPost(ctx, a.lspClient, a.cfg.ListConnectionsPath, &raw); err != nil {
		return nil, err
	}

	var cResp listConnectionsResponse
	if err := json.Unmarshal(raw, &cResp); err == nil && cResp.Connections != nil {
		return cResp.Connections, nil
	}

	var pResp listPeersResponse
	if err := json.Unmarshal(raw, &pResp); err == nil && pResp.Peers != nil {
		conns := make([]Connection, 0, len(pResp.Peers))
		for _, p := range pResp.Peers {
			conns = append(conns, Connection{
				PeerPubkeyAndOptAddr: p.Pubkey,
				CapacitySat:          a.cfg.DefaultChannelCapacitySat,
				PushMsat:             a.cfg.DefaultChannelPushMsat,
				Public:               false,
				WithAnchors:          true,
			})
		}
		return conns, nil
	}

	return nil, errors.New("list connections response did not match known schemas")
}

func (a *API) cancelLNInvoice(ctx context.Context, lnInvoice string) {
	if a.cfg.CancelLNInvoicePath == "" {
		return
	}
	_ = a.lspClient.DoJSON(ctx, http.MethodPost, a.cfg.CancelLNInvoicePath, map[string]any{"invoice": lnInvoice}, nil)
}

func (a *API) getOrPost(ctx context.Context, client *NodeClient, path string, out any) error {
	err := client.DoJSON(ctx, http.MethodGet, path, nil, out)
	if err == nil {
		return nil
	}
	if hErr, ok := err.(*HTTPError); ok && (hErr.StatusCode == http.StatusMethodNotAllowed || hErr.StatusCode == http.StatusNotFound) {
		return client.DoJSON(ctx, http.MethodPost, path, map[string]any{}, out)
	}
	return err
}

func (a *API) openChannelPayload(c Connection) (any, error) {
	inbound := uint64(0)
	if c.AssetDecimals != nil {
		mul := uint64(1)
		for i := 0; i < int(*c.AssetDecimals); i++ {
			mul *= 10
		}
		if mul > 0 {
			inbound = 1_000_000 * mul
		}
	}

	if len(c.OpenChannelParams) > 0 {
		var payload map[string]any
		if err := json.Unmarshal(c.OpenChannelParams, &payload); err != nil {
			return nil, err
		}
		if inbound > 0 {
			if _, ok := payload["push_asset_amount"]; !ok {
				payload["push_asset_amount"] = inbound
			}
		}
		return payload, nil
	}

	req := OpenChannelRequest{
		PeerPubkeyAndOptAddr: c.PeerPubkeyAndOptAddr,
		CapacitySat:          c.CapacitySat,
		PushMsat:             c.PushMsat,
		AssetID:              c.AssetID,
		Public:               c.Public,
		WithAnchors:          c.WithAnchors,
	}
	if inbound > 0 {
		req.PushAssetAmount = &inbound
	}
	return req, nil
}

func channelKey(peer string, assetID *string) string {
	asset := ""
	if assetID != nil {
		asset = *assetID
	}
	return peer + "|" + asset
}

func peerOnly(peerPubkeyAndOptAddr string) string {
	parts := strings.SplitN(peerPubkeyAndOptAddr, "@", 2)
	return parts[0]
}

func normalizeStatus(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func unixFromTimestampAndExpiry(ts, exp uint64) time.Time {
	return time.Unix(int64(ts+exp), 0).UTC()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRawJSON(w http.ResponseWriter, code int, raw []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(raw)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}
