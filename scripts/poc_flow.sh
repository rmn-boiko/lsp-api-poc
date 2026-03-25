#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

API_BASE_URL="${API_BASE_URL:-http://127.0.0.1:8080}"
NODE_BASE_URL="${NODE_BASE_URL:-http://127.0.0.1:3001}"
DB_PATH="${DB_PATH:-$PROJECT_ROOT/lsp_api_poc.db}"
WAIT_SECONDS="${WAIT_SECONDS:-15}"

# Optional bearer token for rgb-lightning-node.
NODE_TOKEN="${NODE_TOKEN:-}"
NODE_PASSWORD="${NODE_PASSWORD:-password123}"
NODE_MNEMONIC="${NODE_MNEMONIC:-}"

# Unlock params (regtest defaults from rgb-lightning-node docs).
BITCOIND_RPC_USERNAME="${BITCOIND_RPC_USERNAME:-user}"
BITCOIND_RPC_PASSWORD="${BITCOIND_RPC_PASSWORD:-password}"
BITCOIND_RPC_HOST="${BITCOIND_RPC_HOST:-localhost}"
BITCOIND_RPC_PORT="${BITCOIND_RPC_PORT:-18443}"
INDEXER_URL="${INDEXER_URL:-127.0.0.1:50001}"
PROXY_ENDPOINT="${PROXY_ENDPOINT:-rpc://127.0.0.1:3000/json-rpc}"
ANNOUNCE_ADDRESSES="${ANNOUNCE_ADDRESSES:-[]}"
ANNOUNCE_ALIAS="${ANNOUNCE_ALIAS:-null}"

# Inputs for flows.
ASSET_ID="${ASSET_ID:-}"
USER_LN_INVOICE="${USER_LN_INVOICE:-}"
USER_RGB_INVOICE="${USER_RGB_INVOICE:-}"
LN_AMT_MSAT="${LN_AMT_MSAT:-3000000}"
LN_EXPIRY_SEC="${LN_EXPIRY_SEC:-3600}"
RGB_DURATION_SECONDS="${RGB_DURATION_SECONDS:-3600}"
RGB_MIN_CONFIRMATIONS="${RGB_MIN_CONFIRMATIONS:-1}"
RGB_WITNESS="${RGB_WITNESS:-false}"
SENDRGB_FEE_RATE="${SENDRGB_FEE_RATE:-1}"
SENDRGB_SKIP_SYNC="${SENDRGB_SKIP_SYNC:-false}"
RGB_PAY_AMOUNT="${RGB_PAY_AMOUNT:-1}"

# Auto-pay toggles for test automation.
AUTO_PAY_LN="${AUTO_PAY_LN:-false}"
AUTO_PAY_RGB="${AUTO_PAY_RGB:-false}"

# Optional peer bootstrap.
PEER_PUBKEY_AND_ADDR="${PEER_PUBKEY_AND_ADDR:-}"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing required command: $1"; exit 1; }
}

node_curl() {
  local method="$1"; shift
  local path="$1"; shift
  local data="${1:-}"
  local auth=()
  local tmp http_code curl_rc
  if [[ -n "$NODE_TOKEN" ]]; then
    auth=(-H "Authorization: Bearer $NODE_TOKEN")
  fi
  tmp="$(mktemp)"
  if [[ -n "$data" ]]; then
    set +e
    http_code="$(curl -sS -o "$tmp" -w '%{http_code}' -X "$method" "$NODE_BASE_URL$path" "${auth[@]}" -H 'content-type: application/json' -d "$data")"
    curl_rc=$?
    set -e
  else
    set +e
    http_code="$(curl -sS -o "$tmp" -w '%{http_code}' -X "$method" "$NODE_BASE_URL$path" "${auth[@]}")"
    curl_rc=$?
    set -e
  fi
  cat "$tmp" 2>/dev/null || true
  rm -f "$tmp"
  if [[ "$curl_rc" -ne 0 ]]; then
    return "$curl_rc"
  fi
  if [[ "$http_code" -ge 400 ]]; then
    return 22
  fi
}

api_curl() {
  local method="$1"; shift
  local path="$1"; shift
  local data="${1:-}"
  local tmp http_code curl_rc
  tmp="$(mktemp)"
  if [[ -n "$data" ]]; then
    set +e
    http_code="$(curl -sS -o "$tmp" -w '%{http_code}' -X "$method" "$API_BASE_URL$path" -H 'content-type: application/json' -d "$data")"
    curl_rc=$?
    set -e
  else
    set +e
    http_code="$(curl -sS -o "$tmp" -w '%{http_code}' -X "$method" "$API_BASE_URL$path")"
    curl_rc=$?
    set -e
  fi
  cat "$tmp" 2>/dev/null || true
  rm -f "$tmp"
  if [[ "$curl_rc" -ne 0 ]]; then
    return "$curl_rc"
  fi
  if [[ "$http_code" -ge 400 ]]; then
    return 22
  fi
}

extract_json_field() {
  local json="$1"
  local jq_expr="$2"
  if command -v jq >/dev/null 2>&1; then
    printf '%s' "$json" | jq -r "$jq_expr"
  else
    # Minimal fallback for string fields only.
    local field
    field="$(printf '%s' "$jq_expr" | sed 's/^\.//')"
    printf '%s' "$json" | sed -n "s/.*\"$field\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p"
  fi
}

pay_ln_invoice() {
  local invoice="$1"
  if [[ -z "$invoice" ]]; then
    echo "pay_ln_invoice: missing invoice"
    return 1
  fi
  echo "== Paying LN invoice via /sendpayment =="
  node_curl POST /sendpayment "{\"invoice\":\"$invoice\"}"
  echo
}

pay_rgb_invoice() {
  local invoice="$1"
  if [[ -z "$invoice" ]]; then
    echo "pay_rgb_invoice: missing invoice"
    return 1
  fi
  need_cmd jq

  echo "== Decoding RGB invoice =="
  local dec
  dec="$(node_curl POST /decodergbinvoice "{\"invoice\":\"$invoice\"}")"
  echo "$dec"
  echo

  local asset_id recipient_id
  asset_id="$(printf '%s' "$dec" | jq -r '.asset_id // empty')"
  recipient_id="$(printf '%s' "$dec" | jq -r '.recipient_id // empty')"
  if [[ -z "$asset_id" || -z "$recipient_id" ]]; then
    echo "pay_rgb_invoice: could not extract asset_id/recipient_id from decoded invoice"
    return 1
  fi

  local assignment_json transport_json
  assignment_json="$(printf '%s' "$dec" | jq -c '.assignment')"
  if printf '%s' "$assignment_json" | jq -e '.type=="Fungible" and ((.value // 0) == 0)' >/dev/null; then
    assignment_json="$(jq -n --argjson v "$RGB_PAY_AMOUNT" '{type:"Fungible",value:$v}')"
  fi
  transport_json="$(printf '%s' "$dec" | jq -c '.transport_endpoints // []')"

  echo "== Paying RGB invoice via /sendrgb =="
  local payload
  payload="$(jq -n \
    --arg asset_id "$asset_id" \
    --arg recipient_id "$recipient_id" \
    --argjson assignment "$assignment_json" \
    --argjson transport "$transport_json" \
    --argjson fee_rate "$SENDRGB_FEE_RATE" \
    --argjson min_conf "$RGB_MIN_CONFIRMATIONS" \
    --argjson skip_sync "$SENDRGB_SKIP_SYNC" \
    '{
      donation: false,
      fee_rate: $fee_rate,
      min_confirmations: $min_conf,
      skip_sync: $skip_sync,
      recipient_map: {
        ($asset_id): [
          {
            recipient_id: $recipient_id,
            assignment: $assignment,
            transport_endpoints: $transport
          }
        ]
      }
    }')"
  node_curl POST /sendrgb "$payload"
  echo
}

preflight() {
  need_cmd curl
  need_cmd sqlite3

  echo "== API health =="
  api_curl GET /health
  echo

  echo "== Node listpeers =="
  node_curl GET /listpeers
  echo

  echo "== Node listchannels =="
  node_curl GET /listchannels
  echo
}

node_initial() {
  echo "== Node initial requests =="
  echo "-- /networkinfo"
  node_curl GET /networkinfo || true
  echo
  echo "-- /nodeinfo"
  node_curl GET /nodeinfo || true
  echo
  echo "-- /listpeers"
  node_curl GET /listpeers
  echo
  echo "-- /listchannels"
  node_curl GET /listchannels
  echo

  if [[ -n "$PEER_PUBKEY_AND_ADDR" ]]; then
    echo "-- /connectpeer"
    node_curl POST /connectpeer "{\"peer_pubkey_and_addr\":\"$PEER_PUBKEY_AND_ADDR\"}"
    echo
  fi
}

node_init() {
  echo "== Node init =="
  local payload
  if [[ -n "$NODE_MNEMONIC" ]]; then
    payload="{\"password\":\"$NODE_PASSWORD\",\"mnemonic\":\"$NODE_MNEMONIC\"}"
  else
    payload="{\"password\":\"$NODE_PASSWORD\"}"
  fi
  node_curl POST /init "$payload"
  echo
}

node_unlock() {
  echo "== Node unlock =="
  local payload
  payload=$(cat <<JSON
{
  "password":"$NODE_PASSWORD",
  "bitcoind_rpc_username":"$BITCOIND_RPC_USERNAME",
  "bitcoind_rpc_password":"$BITCOIND_RPC_PASSWORD",
  "bitcoind_rpc_host":"$BITCOIND_RPC_HOST",
  "bitcoind_rpc_port":$BITCOIND_RPC_PORT,
  "indexer_url":"$INDEXER_URL",
  "proxy_endpoint":"$PROXY_ENDPOINT",
  "announce_addresses":$ANNOUNCE_ADDRESSES,
  "announce_alias":$ANNOUNCE_ALIAS
}
JSON
)
  node_curl POST /unlock "$payload"
  echo
}

tolerant_unlock() {
  set +e
  local resp
  resp="$(node_unlock 2>&1)"
  local rc=$?
  set -e
  if [[ $rc -eq 0 ]]; then
    return 0
  fi
  if printf '%s' "$resp" | grep -qiE "already.*unlocked|AlreadyUnlocked|UnlockedNode"; then
    echo "Node already unlocked, continuing."
    return 0
  fi
  printf '%s\n' "$resp"
  return $rc
}

lightning_receive() {
  if [[ -z "$ASSET_ID" || -z "$USER_LN_INVOICE" ]]; then
    echo "ASSET_ID and USER_LN_INVOICE are required for lightning-receive"
    exit 1
  fi

  local payload
  payload=$(cat <<JSON
{
  "ln_invoice": "$USER_LN_INVOICE",
  "rgb_invoice": {
    "asset_id": "$ASSET_ID",
    "duration_seconds": $RGB_DURATION_SECONDS,
    "min_confirmations": $RGB_MIN_CONFIRMATIONS,
    "witness": $RGB_WITNESS
  }
}
JSON
)

  echo "== POST /lightning_receive =="
  local resp
  resp="$(api_curl POST /lightning_receive "$payload")"
  echo "$resp"
  echo

  local mapping
  if command -v jq >/dev/null 2>&1; then
    mapping="$(printf '%s' "$resp" | jq -r '.mapping_id // empty')"
  else
    mapping="$(printf '%s' "$resp" | sed -n 's/.*"mapping_id"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p')"
  fi
  if [[ -n "$mapping" ]]; then
    echo "lightning_receive mapping_id=$mapping"
  fi

  if [[ "$AUTO_PAY_RGB" == "true" ]]; then
    local rgb_invoice
    rgb_invoice="$(extract_json_field "$resp" '.rgb_invoice')"
    if [[ -n "$rgb_invoice" && "$rgb_invoice" != "null" ]]; then
      pay_rgb_invoice "$rgb_invoice"
    else
      echo "AUTO_PAY_RGB enabled but rgb_invoice missing in response"
    fi
  fi
}

onchain_send() {
  if [[ -z "$USER_RGB_INVOICE" ]]; then
    echo "USER_RGB_INVOICE is required for onchain-send"
    exit 1
  fi

  local payload
  payload=$(cat <<JSON
{
  "rgb_invoice": "$USER_RGB_INVOICE",
  "lninvoice": {
    "amt_msat": $LN_AMT_MSAT,
    "expiry_sec": $LN_EXPIRY_SEC
  }
}
JSON
)

  echo "== POST /onchain_send =="
  local resp
  resp="$(api_curl POST /onchain_send "$payload")"
  echo "$resp"
  echo

  if command -v jq >/dev/null 2>&1; then
    local ln_invoice
    ln_invoice="$(printf '%s' "$resp" | jq -r '.ln_invoice // empty')"
    if [[ -n "$ln_invoice" ]]; then
      echo "Pay this LN invoice to progress flow:"
      echo "$ln_invoice"
      if [[ "$AUTO_PAY_LN" == "true" ]]; then
        pay_ln_invoice "$ln_invoice"
      fi
    fi
  fi
}

monitor() {
  echo "Sleeping ${WAIT_SECONDS}s for cron ticks..."
  sleep "$WAIT_SECONDS"

  echo "== DB onchain_send_mappings =="
  sqlite3 "$DB_PATH" "select id,status,datetime(created_at),coalesce(last_error,'') from onchain_send_mappings order by id desc limit 10;"
  echo

  echo "== DB lightning_receive_mappings =="
  sqlite3 "$DB_PATH" "select id,status,rgb_asset_id,batch_transfer_idx,datetime(created_at),coalesce(last_error,'') from lightning_receive_mappings order by id desc limit 10;"
  echo

  if [[ -n "$ASSET_ID" ]]; then
    echo "== Node refreshtransfers + listtransfers(asset_id=$ASSET_ID) =="
    node_curl POST /refreshtransfers '{"skip_sync":false}'
    echo
    node_curl POST /listtransfers "{\"asset_id\":\"$ASSET_ID\"}"
    echo
  fi
}

all() {
  tolerant_unlock
  preflight
  node_initial
  if [[ -n "$USER_LN_INVOICE" && -n "$ASSET_ID" ]]; then
    lightning_receive
  fi
  if [[ -n "$USER_RGB_INVOICE" ]]; then
    onchain_send
  fi
  monitor
}

usage() {
  cat <<USAGE
Usage: $(basename "$0") <command>

Commands:
  preflight          Check API health and basic node endpoints.
  node-init          Call node /init (optional one-time setup).
  node-unlock        Call node /unlock.
  node-initial       Run initial node requests (and optional connectpeer).
  lightning-receive  Call PoC /lightning_receive.
  pay-ln            Pay LN invoice via node /sendpayment (requires LN_INVOICE env).
  pay-rgb           Pay RGB invoice via node /sendrgb (requires RGB_INVOICE env).
  onchain-send       Call PoC /onchain_send.
  monitor            Wait and inspect DB + transfer status.
  all                Run full sequence.

Environment variables:
  API_BASE_URL, NODE_BASE_URL, NODE_TOKEN, DB_PATH, WAIT_SECONDS
  NODE_PASSWORD, NODE_MNEMONIC
  BITCOIND_RPC_USERNAME, BITCOIND_RPC_PASSWORD, BITCOIND_RPC_HOST, BITCOIND_RPC_PORT
  INDEXER_URL, PROXY_ENDPOINT, ANNOUNCE_ADDRESSES, ANNOUNCE_ALIAS
  ASSET_ID, USER_LN_INVOICE, USER_RGB_INVOICE
  LN_INVOICE, RGB_INVOICE
  LN_AMT_MSAT, LN_EXPIRY_SEC
  RGB_DURATION_SECONDS, RGB_MIN_CONFIRMATIONS, RGB_WITNESS
  SENDRGB_FEE_RATE, SENDRGB_SKIP_SYNC
  RGB_PAY_AMOUNT
  AUTO_PAY_LN, AUTO_PAY_RGB
  PEER_PUBKEY_AND_ADDR
USAGE
}

main() {
  local cmd="${1:-}"
  case "$cmd" in
    preflight) preflight ;;
    node-init) node_init ;;
    node-unlock) node_unlock ;;
    node-initial) node_initial ;;
    lightning-receive) lightning_receive ;;
    pay-ln)
      if [[ -z "${LN_INVOICE:-}" ]]; then
        echo "LN_INVOICE is required"
        exit 1
      fi
      pay_ln_invoice "$LN_INVOICE"
      ;;
    pay-rgb)
      if [[ -z "${RGB_INVOICE:-}" ]]; then
        echo "RGB_INVOICE is required"
        exit 1
      fi
      pay_rgb_invoice "$RGB_INVOICE"
      ;;
    onchain-send) onchain_send ;;
    monitor) monitor ;;
    all) all ;;
    *) usage; exit 1 ;;
  esac
}

main "$@"
