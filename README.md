# lsp-api-poc

POC bridge API for RGB + Lightning LSP workflows.

## Endpoints

- `GET /health`
- `GET /get_info`
- `POST /onchain_send`
- `POST /lightning_receive`

## Request examples

`/onchain_send`

```json
{
  "rgb_invoice": "rgb1...",
  "lninvoice": {
    "amt_msat": 3000000,
    "expiry_sec": 3600,
    "asset_id": "...",
    "asset_amount": 1000
  }
}
```

`/lightning_receive`

```json
{
  "ln_invoice": "lnbc...",
  "rgb_invoice": {
    "asset_id": "...",
    "assignment": "Value",
    "duration_seconds": 3600,
    "min_confirmations": 1,
    "witness": false
  }
}
```

`rgb_invoice.asset_id` is required so the cron monitor can query `listtransfers` deterministically.

## Cron jobs

Runs every `CRON_EVERY` (default `30s`):

1. `listpeers` + `listchannels` and auto `openchannel` if channel is missing.
2. Monitor LN invoices from `onchain_send`; if paid, execute `sendrgb`.
3. Monitor RGB invoices from `lightning_receive`; if paid, execute `sendpayment`.
4. Mark expired unpaid invoices as `expired` and optionally call cancel endpoint.

## Method mapping and RGB status flow

This POC uses `rgb-lightning-node` HTTP routes as the reference API.

### Method mapping used in this POC

- `listconnections` -> `listpeers`
- `openconnection` -> `connectpeer` (or skip explicit connect and rely on `openchannel` auto-connect logic)
- `sendln` -> `sendpayment`
- `rgbinvoicestatus` -> `refreshtransfers` + `listtransfers` (matched by `batch_transfer_idx`)

### Why `refreshtransfers + listtransfers` is used

There is no direct `rgbinvoicestatus` route in `rgb-lightning-node`.
Instead:

1. `POST /rgbinvoice` returns `batch_transfer_idx` and `expiration_timestamp`.
2. `POST /refreshtransfers` updates pending transfer state in wallet storage.
3. `POST /listtransfers` returns current transfer states for a specific `asset_id`.
4. The transfer with `idx == batch_transfer_idx` is treated as the tracked invoice transfer.

Relevant statuses:

- `WaitingCounterparty`
- `WaitingConfirmations`
- `Settled`
- `Failed`

### Cron logic for lightning receive (`ln -> rgb -> sendpayment`)

For each pending record:

1. If saved `rgb_expires_at` is in the past: mark `expired`.
2. Call `refreshtransfers`.
3. Call `listtransfers` for saved `asset_id`.
4. Find transfer with saved `batch_transfer_idx`.
5. If transfer status is `Settled`: call `sendpayment` with saved user LN invoice and mark `completed`.
6. If transfer status is `Failed`: mark `failed`.
7. If transfer is missing or still waiting: keep as pending and retry next tick.

### Data that must be persisted per lightning-receive mapping

To make step 4 deterministic, store these fields when creating `rgbinvoice`:

- User LN invoice (for final `sendpayment`)
- LSP RGB invoice string
- `batch_transfer_idx` from `rgbinvoice` response
- `asset_id` used for `listtransfers`
- `expiration_timestamp` (as `rgb_expires_at`)

Without `batch_transfer_idx + asset_id`, the monitor cannot reliably identify which transfer to evaluate.

## Environment variables

- `SERVER_ADDR` default `:8080`
- `DATABASE_DRIVER` `sqlite` (default) or `postgres`
- `DATABASE_URL` default `lsp_api_poc.db`
- `LSP_BASE_URL` default `http://127.0.0.1:3001`
- `LSP_TOKEN` optional bearer token
- `RGB_NODE_BASE_URL` default `LSP_BASE_URL`
- `RGB_NODE_TOKEN` optional bearer token
- `HTTP_TIMEOUT` default `15s`
- `CRON_EVERY` default `30s`
- `LSP_GET_INFO_PATH` default `/nodeinfo`
- `LSP_OPENCONNECTION_PATH` default `/connectpeer`
- `LSP_LISTCONNECTIONS_PATH` default `/listpeers`
- `LSP_LISTCHANNELS_PATH` default `/listchannels`
- `LSP_OPENCHANNEL_PATH` default `/openchannel`
- `LSP_LNINVOICE_PATH` default `/lninvoice`
- `LSP_INVOICESTATUS_PATH` default `/invoicestatus`
- `LSP_CANCELLNINVOICE_PATH` optional
- `LSP_SENDRGB_PATH` default `/sendrgb`
- `LSP_SENDLN_PATH` default `/sendpayment`
- `RGB_DECODE_LN_PATH` default `/decodelninvoice`
- `RGB_DECODE_RGB_PATH` default `/decodergbinvoice`
- `RGB_INVOICE_PATH` default `/rgbinvoice`
- `RGB_REFRESH_TRANSFERS_PATH` default `/refreshtransfers`
- `RGB_LIST_TRANSFERS_PATH` default `/listtransfers`
- `DEFAULT_CHANNEL_CAPACITY_SAT` default `200000` (used when `listpeers` does not provide channel params)
- `DEFAULT_CHANNEL_PUSH_MSAT` default `0`

## Test flow

### 1) Start dependencies

1. Start `rgb-lightning-node` and make sure it is unlocked.
2. Make sure the node has at least one funded wallet/asset setup needed for your test case.

Example (regtest) from your local workspace:

```bash
# shell 1: start regtest services
./regtest.sh start

# shell 2: start rgb-lightning-node API daemon on :3001
rgb-lightning-node dataldk0/ --daemon-listening-port 3001 \
  --ldk-peer-listening-port 9735 --network regtest \
  --disable-authentication
```

If you do not have the binary in `PATH`, run from source:

```bash
cargo run -- dataldk0/ --daemon-listening-port 3001 \
  --ldk-peer-listening-port 9735 --network regtest \
  --disable-authentication
```

### 2) Start this API

```bash
export LSP_BASE_URL="http://127.0.0.1:3001"
export RGB_NODE_BASE_URL="http://127.0.0.1:3001"
export CRON_EVERY="10s"
go run .
```

Health check:

```bash
curl -s http://127.0.0.1:8080/health
```

### 3) Test `lightning_receive` flow (`ln -> rgb -> sendpayment`)

1. Create or provide a valid LN invoice from the final receiver.
2. Call:

```bash
curl -s -X POST http://127.0.0.1:8080/lightning_receive \
  -H 'content-type: application/json' \
  -d '{
    "ln_invoice":"<USER_LN_INVOICE>",
    "rgb_invoice":{
      "asset_id":"<ASSET_ID>",
      "assignment":"Value",
      "duration_seconds":3600,
      "min_confirmations":1,
      "witness":false
    }
  }'
```

Expected response:

- `rgb_invoice` present
- `mapping_id` present

3. Pay the returned RGB invoice from sender side.
4. Wait for at least one cron tick.
5. Verify status in DB:

```bash
sqlite3 lsp_api_poc.db "select id,status,rgb_asset_id,batch_transfer_idx,created_at from lightning_receive_mappings order by id desc limit 5;"
```

Expected transition: `pending_rgb -> completed` (or `failed`/`expired` depending on payment outcome).

### 4) Test `onchain_send` flow (`rgb -> ln -> sendrgb`)

1. Create or provide a valid RGB invoice from the final receiver.
2. Call:

```bash
curl -s -X POST http://127.0.0.1:8080/onchain_send \
  -H 'content-type: application/json' \
  -d '{
    "rgb_invoice":"<USER_RGB_INVOICE>",
    "lninvoice":{
      "amt_msat":3000000,
      "expiry_sec":3600
    }
  }'
```

Expected response:

- `ln_invoice` present
- `mapping_id` present

3. Pay the returned LN invoice.
4. Wait for at least one cron tick.
5. Verify status in DB:

```bash
sqlite3 lsp_api_poc.db "select id,status,created_at from onchain_send_mappings order by id desc limit 5;"
```

Expected transition: `pending_ln -> completed` (or `failed`/`expired`).

### 5) Troubleshooting checks

- If `lightning_receive` never completes:
  - check `POST /refreshtransfers` and `POST /listtransfers` responses on `rgb-lightning-node`
  - ensure `asset_id` is correct and matches transfer records
- If `POST /lninvoice` returns EOF / empty reply:
  - verify bitcoind RPC port matches your compose (`18443` in this repo)
  - ensure node data dir contains `.ldk/` (e.g. `dataldk0/.ldk`)
  - restart node after creating missing `.ldk` directory
- If channel auto-open fails in cron:
  - verify peers are connected (`GET /listpeers`)
  - verify `DEFAULT_CHANNEL_CAPACITY_SAT`/`DEFAULT_CHANNEL_PUSH_MSAT` are valid for your node policy

## Test script

You can automate the flow with:

- [`scripts/poc_flow.sh`](/home/roman-boiko/projects/utexo/lsp-api-poc/scripts/poc_flow.sh)

Quick usage:

```bash
# optional one-time init (skip if already initialized)
NODE_PASSWORD="password123" ./scripts/poc_flow.sh node-init

# unlock node (required before preflight/cron calls)
NODE_PASSWORD="password123" \
BITCOIND_RPC_USERNAME="user" \
BITCOIND_RPC_PASSWORD="password" \
BITCOIND_RPC_HOST="localhost" \
BITCOIND_RPC_PORT=18443 \
INDEXER_URL="127.0.0.1:50001" \
PROXY_ENDPOINT="rpc://127.0.0.1:3000/json-rpc" \
./scripts/poc_flow.sh node-unlock

./scripts/poc_flow.sh preflight
./scripts/poc_flow.sh node-initial
```

Run `lightning_receive` test:

```bash
cd /home/roman-boiko/projects/utexo/lsp-api-poc
ASSET_ID="<ASSET_ID>" \
USER_LN_INVOICE="<USER_LN_INVOICE>" \
AUTO_PAY_RGB=true \
./scripts/poc_flow.sh lightning-receive
./scripts/poc_flow.sh monitor
```

Run `onchain_send` test:

```bash
cd /home/roman-boiko/projects/utexo/lsp-api-poc
USER_RGB_INVOICE="<USER_RGB_INVOICE>" \
LN_AMT_MSAT=3000000 \
LN_EXPIRY_SEC=3600 \
AUTO_PAY_LN=true \
./scripts/poc_flow.sh onchain-send
./scripts/poc_flow.sh monitor
```

Run all steps in one command (when all required envs are set):

```bash
cd /home/roman-boiko/projects/utexo/lsp-api-poc
NODE_PASSWORD="password123" \
BITCOIND_RPC_USERNAME="user" \
BITCOIND_RPC_PASSWORD="password" \
BITCOIND_RPC_HOST="localhost" \
BITCOIND_RPC_PORT=18443 \
INDEXER_URL="127.0.0.1:50001" \
PROXY_ENDPOINT="rpc://127.0.0.1:3000/json-rpc" \
ASSET_ID="<ASSET_ID>" \
USER_LN_INVOICE="<USER_LN_INVOICE>" \
USER_RGB_INVOICE="<USER_RGB_INVOICE>" \
AUTO_PAY_LN=true \
AUTO_PAY_RGB=true \
WAIT_SECONDS=20 \
./scripts/poc_flow.sh all
```

`all` attempts a tolerant unlock first (continues if node is already unlocked).

Optional envs for node auth/bootstrap:

- `NODE_TOKEN` for bearer auth
- `NODE_PASSWORD`, `NODE_MNEMONIC`
- `BITCOIND_RPC_USERNAME`, `BITCOIND_RPC_PASSWORD`, `BITCOIND_RPC_HOST`, `BITCOIND_RPC_PORT`
- `INDEXER_URL`, `PROXY_ENDPOINT`, `ANNOUNCE_ADDRESSES`, `ANNOUNCE_ALIAS`
- `PEER_PUBKEY_AND_ADDR` to call `/connectpeer` in `node-initial`
- `API_BASE_URL`, `NODE_BASE_URL`, `DB_PATH`, `WAIT_SECONDS`
- `AUTO_PAY_LN`, `AUTO_PAY_RGB` to auto-pay generated invoices during flow
- `RGB_PAY_AMOUNT` default `1` (used when decoded RGB invoice assignment is fungible `0`)

Standalone payment helpers:

```bash
# Pay LN invoice directly through rgb-lightning-node
LN_INVOICE="<LN_INVOICE>" ./scripts/poc_flow.sh pay-ln

# Pay RGB invoice directly through rgb-lightning-node
RGB_INVOICE="<RGB_INVOICE>" ./scripts/poc_flow.sh pay-rgb
```

## Run

```bash
go run .
```
