#!/bin/bash
set -e
cd /root/ant2api/new

BIN="./bin/refactor-server"
PPROF_URL="http://localhost:6060/debug/pprof"
RESULTS_DIR="./benchmark_results"
mkdir -p "$RESULTS_DIR"

TEST_REQUESTS=10

run_test() {
    local mode=$1
    local debug_val=$2
    echo "========== 测试: $mode =========="
    
    # 修改.env
    sed -i '/^DEBUG=/d' .env
    sed -i '/^#DEBUG=/d' .env
    [ -n "$debug_val" ] && echo "DEBUG=$debug_val" >> .env
    
    $BIN &
    PID=$!
    sleep 3
    
    MEM_BEFORE=$(awk '/VmRSS/{print $2}' /proc/$PID/status)
    
    # CPU profile 5秒
    curl -s "$PPROF_URL/profile?seconds=5" -o "$RESULTS_DIR/cpu_$mode.prof" &
    
    START=$(date +%s%N)
    for i in $(seq 1 $TEST_REQUESTS); do
        curl -s -X POST http://localhost:8045/v1/chat/completions \
            -H "Content-Type: application/json" \
            -H "x-api-key: sk-123456" \
            -d '{"model":"gemini-2.0-flash","messages":[{"role":"user","content":"hi"}],"max_tokens":5}' &
    done
    wait
    END=$(date +%s%N)
    
    curl -s "$PPROF_URL/heap" -o "$RESULTS_DIR/heap_$mode.prof"
    curl -s "$PPROF_URL/allocs" -o "$RESULTS_DIR/allocs_$mode.prof"
    
    MEM_AFTER=$(awk '/VmRSS/{print $2}' /proc/$PID/status)
    DURATION=$(( (END - START) / 1000000 ))
    
    echo "  耗时: ${DURATION}ms | 内存: ${MEM_BEFORE}KB -> ${MEM_AFTER}KB (+$((MEM_AFTER-MEM_BEFORE))KB)"
    echo "$mode,$DURATION,$MEM_BEFORE,$MEM_AFTER" >> "$RESULTS_DIR/summary.csv"
    
    kill $PID 2>/dev/null; sleep 2
}

echo "mode,duration_ms,mem_before_kb,mem_after_kb" > "$RESULTS_DIR/summary.csv"

run_test "debug_off" ""
run_test "debug_high" "high"

echo ""
echo "===== 完成 ====="
cat "$RESULTS_DIR/summary.csv"
