#!/bin/bash
# ghttp 基准测试运行脚本
# 使用 bombardier 测试各框架的 /hello 吞吐量和延迟

set -e

BENCH_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="$BENCH_DIR/gowebbenchmark.exe"
RESULT_DIR="$BENCH_DIR/results"
FRAMEWORKS=("default" "gin" "chi" "echo" "gorestful" "fasthttp" "don" "ghttp")

# bombardier
BOMBARDIER="${BOMBARDIER:-$HOME/go/bin/bombardier}"
# 并发连接数
CONCURRENCY="${CONCURRENCY:-500}"
# 连接数（bombardier 用 -c）
CONNECTIONS="${CONNECTIONS:-125}"
# 测试时长（秒）
DURATION="${DURATION:-15}"

mkdir -p "$RESULT_DIR"

echo "=========================================="
echo "  Go HTTP Framework Benchmark"
echo "=========================================="
echo "Concurrency: $CONCURRENCY"
echo "Connections: $CONNECTIONS"
echo "Duration:   ${DURATION}s"
echo "=========================================="

run_bench() {
    local framework=$1
    local sleep_ms=${2:-0}
    local label="${framework}"
    local result_file="$RESULT_DIR/${label}.txt"

    echo ""
    echo "---- Testing: $label ----"
    echo ""

    # Start the server in background
    "$BINARY" "$framework" "$sleep_ms" &
    local server_pid=$!
    sleep 3

    # Run bombardier
    "$BOMBARDIER" -c "$CONNECTIONS" -n "$((CONNECTIONS * DURATION * 100))" \
        -l "http://127.0.0.1:8080/hello" \
        2>&1 | tee "$result_file"

    # Kill server
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
    sleep 2
}

# === Sleep=0ms (pure routing) ===
echo ""
echo "=========================================="
echo "  Benchmark: 0ms sleep (pure routing)"
echo "=========================================="

for fw in "${FRAMEWORKS[@]}"; do
    run_bench "$fw" 0
done

# === 对比结果汇总 ===
echo ""
echo "=========================================="
echo "  Results Summary"
echo "=========================================="
echo ""
printf "%-15s %-25s %-25s\n" "Framework" "Requests/sec" "Latency (avg)"
printf "%-15s %-25s %-25s\n" "--------" "------------" "-------------"

for fw in "${FRAMEWORKS[@]}"; do
    requests=$(grep "Requests/sec" "$RESULT_DIR/${fw}.txt" 2>/dev/null | awk '{print $2}' | head -1)
    latency=$(grep "Latency" "$RESULT_DIR/${fw}.txt" 2>/dev/null | head -1 | awk '{print $2}')
    [ -z "$requests" ] && requests="N/A"
    [ -z "$latency" ] && latency="N/A"
    printf "%-15s %-25s %-25s\n" "$fw" "$requests" "$latency"
done

echo ""
echo "Results saved to: $RESULT_DIR/"
echo "Done."
