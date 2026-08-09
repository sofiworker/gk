#!/usr/bin/env sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
BASE_URL=${BASE_URL:-http://127.0.0.1:18080}; PROFILE=${PROFILE:-smoke}; DURATION=${DURATION:-30s}; SCENARIO=${SCENARIO:-protocol}; OUTPUT_DIR=${OUTPUT_DIR:-$ROOT/results}; SECRET=${SECRET:-known-k6-secret}
case "$BASE_URL" in http://*) ;; *) echo "BASE_URL must use http for the local server" >&2; exit 2;; esac
REST=${BASE_URL#http://}; AUTHORITY=${REST%%/*}; PATH_PART=${REST#"$AUTHORITY"}; [ -z "$PATH_PART" ] || [ "$PATH_PART" = / ] || { echo "BASE_URL path is unsupported" >&2; exit 2; }
case "$REST" in *\?*|*\#*) echo "BASE_URL query and fragment are unsupported" >&2; exit 2;; esac
case "$AUTHORITY" in \[*\]:*) ADDR=$AUTHORITY;; \[*\]) ADDR="$AUTHORITY:80";; *:*:*) echo "IPv6 address must be bracketed" >&2; exit 2;; *:*) ADDR=$AUTHORITY;; *) ADDR="$AUTHORITY:80";; esac
BASE_URL=${BASE_URL%/}; mkdir -p "$OUTPUT_DIR"; TMP=$(mktemp -d); SERVER_PID=; EXIT_CODE=0; PHASE=init; READY=; CLEANUP_FAILED=false
summary(){ printf '{"phase":"%s","scenario":"%s","base_url":"%s","ready_url":"%s","cleanup_failed":%s,"exit_code":%s}\n' "$PHASE" "$SCENARIO" "$BASE_URL" "$READY" "$CLEANUP_FAILED" "$EXIT_CODE" >"$OUTPUT_DIR/run-summary.json"; }
cleanup(){ status=$?; trap - EXIT INT TERM; [ "$EXIT_CODE" -ne 0 ] || EXIT_CODE=$status; if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then kill -TERM "$SERVER_PID" 2>/dev/null||CLEANUP_FAILED=true; i=0; while kill -0 "$SERVER_PID" 2>/dev/null && [ "$i" -lt 20 ]; do sleep .25;i=$((i+1));done; if kill -0 "$SERVER_PID" 2>/dev/null; then kill -KILL "$SERVER_PID" 2>/dev/null||CLEANUP_FAILED=true; fi; wait "$SERVER_PID" 2>/dev/null||true; kill -0 "$SERVER_PID" 2>/dev/null&&CLEANUP_FAILED=true; fi; rm -rf "$TMP"||CLEANUP_FAILED=true; if [ "$CLEANUP_FAILED" = true ] && [ "$EXIT_CODE" -eq 0 ]; then EXIT_CODE=7; PHASE=cleanup; fi; summary; exit "$EXIT_CODE"; }
trap cleanup EXIT; trap 'EXIT_CODE=130; PHASE=signal; exit 130' INT TERM
stage(){ printf '%s\n' "[$(date -u +%FT%TZ)] $1" | tee -a "$OUTPUT_DIR/run.log"; }
PHASE=build; stage build; (cd "$ROOT" && go build -o "$TMP/server" ./cmd/server && go build -o "$TMP/rawprobe" ./cmd/rawprobe && go build -o "$TMP/resources" ./cmd/resources) || { EXIT_CODE=3; exit "$EXIT_CODE"; }
PHASE=server; stage server; : >"$OUTPUT_DIR/server.log"; "$TMP/server" -addr "$ADDR" -static-dir "$ROOT/fixtures/static" -secret "$SECRET" -shutdown-timeout 5s >"$OUTPUT_DIR/server.log" 2>&1 & SERVER_PID=$!
i=0; READY=; while [ "$i" -lt 80 ]; do READY=$(sed -n 's/^READY //p' "$OUTPUT_DIR/server.log" | tail -n 1); [ -n "$READY" ] && break; kill -0 "$SERVER_PID" 2>/dev/null || { echo 'server exited before READY' >&2; EXIT_CODE=3; exit "$EXIT_CODE"; }; sleep .25;i=$((i+1));done; [ -n "$READY" ] || { echo 'READY timeout' >&2;EXIT_CODE=3;exit "$EXIT_CODE"; }
[ "$READY" = "$BASE_URL" ] || { echo "READY mismatch: $READY != $BASE_URL" >&2; EXIT_CODE=3; exit "$EXIT_CODE"; }
i=0; until curl -fsS "$READY/health" >"$OUTPUT_DIR/health.json"; do i=$((i+1)); [ "$i" -lt 40 ]||{ echo 'health timeout' >&2;EXIT_CODE=3;exit "$EXIT_CODE";};sleep .25;done
PHASE=k6; stage k6; BASE_URL="$READY" PROFILE="$PROFILE" DURATION="$DURATION" SECRET="$SECRET" k6 run --summary-export "$OUTPUT_DIR/k6-summary.json" "$ROOT/scenarios/$SCENARIO.js" >"$OUTPUT_DIR/k6.log" 2>&1 || { EXIT_CODE=4; exit "$EXIT_CODE"; }
PHASE=rawprobe; stage rawprobe; "$TMP/rawprobe" -addr "$ADDR" -secret "$SECRET" -health-url "$READY/health" -metrics-url "$READY/__test/metrics" -output "$OUTPUT_DIR/rawprobe.json" >"$OUTPUT_DIR/rawprobe.log" 2>&1 || { EXIT_CODE=5; exit "$EXIT_CODE"; }
PHASE=resources; stage resources; "$TMP/resources" -output "$OUTPUT_DIR/resources.json" >"$OUTPUT_DIR/resources.log" 2>&1 || { EXIT_CODE=6; exit "$EXIT_CODE"; }
PHASE=complete; stage "complete exit=0"
