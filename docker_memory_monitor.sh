#!/bin/bash

# Docker容器内存高峰自动抓取脚本
# 监控内存使用，当超过阈值时自动抓取分析数据
# 
# 用法: 
#   ./docker_memory_monitor.sh <容器名> [阈值MB] [pprof端口]
#   ./docker_memory_monitor.sh ant2api 80 6060
#
# 按 Ctrl+C 停止监控

set -e

CONTAINER="${1:-ant2api}"
THRESHOLD_MB="${2:-80}"
PPROF_PORT="${3:-6060}"
CHECK_INTERVAL=2  # 秒

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}=== Docker内存高峰监控 ===${NC}"
echo "容器: $CONTAINER"
echo "阈值: ${THRESHOLD_MB}MB"
echo "检查间隔: ${CHECK_INTERVAL}s"
echo "按 Ctrl+C 停止"
echo ""

# 验证容器存在
if ! docker ps --format '{{.Names}}' | grep -q "^${CONTAINER}$"; then
    MATCH=$(docker ps --format '{{.Names}}' | grep -i "${CONTAINER}" | head -1)
    if [ -n "$MATCH" ]; then
        CONTAINER="$MATCH"
        echo -e "${YELLOW}使用匹配的容器: $CONTAINER${NC}"
    else
        echo -e "${RED}错误: 找不到容器 $1${NC}"
        docker ps --format "  {{.Names}}"
        exit 1
    fi
fi

# 获取pprof URL
CONTAINER_IP=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$CONTAINER" 2>/dev/null)
MAPPED_PORT=$(docker port "$CONTAINER" "$PPROF_PORT" 2>/dev/null | cut -d: -f2 | head -1)

if [ -n "$MAPPED_PORT" ]; then
    PPROF_URL="http://localhost:$MAPPED_PORT/debug/pprof"
elif [ -n "$CONTAINER_IP" ]; then
    PPROF_URL="http://$CONTAINER_IP:$PPROF_PORT/debug/pprof"
else
    PPROF_URL=""
fi

echo "pprof URL: ${PPROF_URL:-未配置}"
echo ""

CAPTURED=0
MAX_CAPTURES=5

get_memory_mb() {
    # 从docker stats获取内存使用量(MB)
    docker stats "$CONTAINER" --no-stream --format "{{.MemUsage}}" 2>/dev/null | \
        awk '{gsub(/[^0-9.]/," ",$0); print $1}' | \
        awk '{
            if ($0 ~ /G/) { print $0 * 1024 }
            else { print $0 }
        }'
}

capture_profile() {
    local mem_mb=$1
    local timestamp=$(date +%Y%m%d_%H%M%S)
    local output_dir="/tmp/docker_peak_${timestamp}"
    
    mkdir -p "$output_dir"
    
    echo -e "${RED}[!] 内存超过阈值: ${mem_mb}MB > ${THRESHOLD_MB}MB${NC}"
    echo -e "${YELLOW}[*] 开始抓取分析数据...${NC}"
    
    # 保存docker stats
    docker stats "$CONTAINER" --no-stream > "$output_dir/docker_stats.txt" 2>/dev/null
    
    # 保存进程信息
    CONTAINER_PID=$(docker inspect --format '{{.State.Pid}}' "$CONTAINER" 2>/dev/null)
    if [ -n "$CONTAINER_PID" ] && [ -d "/proc/$CONTAINER_PID" ]; then
        cat "/proc/$CONTAINER_PID/status" > "$output_dir/proc_status.txt" 2>/dev/null
        cat "/proc/$CONTAINER_PID/smaps_rollup" > "$output_dir/smaps_rollup.txt" 2>/dev/null
    fi
    
    # 使用docker exec从容器内部抓取pprof（因为pprof通常绑定127.0.0.1）
    echo "  通过docker exec抓取pprof..."
    docker exec "$CONTAINER" wget -q -O - "http://127.0.0.1:$PPROF_PORT/debug/pprof/heap" > "$output_dir/heap.pb.gz" 2>/dev/null || \
    docker exec "$CONTAINER" curl -s "http://127.0.0.1:$PPROF_PORT/debug/pprof/heap" > "$output_dir/heap.pb.gz" 2>/dev/null
    
    docker exec "$CONTAINER" wget -q -O - "http://127.0.0.1:$PPROF_PORT/debug/pprof/allocs" > "$output_dir/allocs.pb.gz" 2>/dev/null || \
    docker exec "$CONTAINER" curl -s "http://127.0.0.1:$PPROF_PORT/debug/pprof/allocs" > "$output_dir/allocs.pb.gz" 2>/dev/null
    
    docker exec "$CONTAINER" wget -q -O - "http://127.0.0.1:$PPROF_PORT/debug/pprof/heap?debug=1" > "$output_dir/heap_debug.txt" 2>/dev/null || \
    docker exec "$CONTAINER" curl -s "http://127.0.0.1:$PPROF_PORT/debug/pprof/heap?debug=1" > "$output_dir/heap_debug.txt" 2>/dev/null
    
    # 快速分析
    if [ -s "$output_dir/heap.pb.gz" ]; then
        echo ""
        echo -e "${BLUE}=== 内存热点 TOP 20 ===${NC}"
        go tool pprof -top 20 "$output_dir/heap.pb.gz" 2>/dev/null | tee "$output_dir/analysis.txt"
    else
        echo "  警告: 无法抓取pprof数据"
    fi
    
    echo ""
    echo -e "${GREEN}[√] 数据已保存到: $output_dir${NC}"
    echo ""
    
    CAPTURED=$((CAPTURED + 1))
    
    if [ $CAPTURED -ge $MAX_CAPTURES ]; then
        echo -e "${YELLOW}已达到最大抓取次数 ($MAX_CAPTURES)，停止监控${NC}"
        exit 0
    fi
    
    # 抓取后等待一段时间，避免重复抓取
    echo "等待30秒后继续监控..."
    sleep 30
}

echo -e "${GREEN}开始监控...${NC}"
echo ""

while true; do
    # 获取当前内存
    MEM_RAW=$(docker stats "$CONTAINER" --no-stream --format "{{.MemUsage}}" 2>/dev/null)
    
    # 解析内存值 (处理 "100MiB / 1GiB" 格式)
    MEM_VALUE=$(echo "$MEM_RAW" | awk '{print $1}')
    
    # 转换为MB
    if echo "$MEM_VALUE" | grep -qi "gib\|gb"; then
        MEM_MB=$(echo "$MEM_VALUE" | sed 's/[^0-9.]//g' | awk '{printf "%.0f", $1 * 1024}')
    elif echo "$MEM_VALUE" | grep -qi "mib\|mb"; then
        MEM_MB=$(echo "$MEM_VALUE" | sed 's/[^0-9.]//g' | awk '{printf "%.0f", $1}')
    elif echo "$MEM_VALUE" | grep -qi "kib\|kb"; then
        MEM_MB=$(echo "$MEM_VALUE" | sed 's/[^0-9.]//g' | awk '{printf "%.0f", $1 / 1024}')
    else
        MEM_MB=$(echo "$MEM_VALUE" | sed 's/[^0-9.]//g')
    fi
    
    # 显示当前状态
    TIMESTAMP=$(date +"%H:%M:%S")
    if [ -n "$MEM_MB" ] && [ "$MEM_MB" -gt 0 ] 2>/dev/null; then
        if [ "$MEM_MB" -gt "$THRESHOLD_MB" ]; then
            echo -e "${TIMESTAMP} | 内存: ${RED}${MEM_MB}MB${NC} (超过阈值 ${THRESHOLD_MB}MB)"
            capture_profile "$MEM_MB"
        else
            echo -e "${TIMESTAMP} | 内存: ${GREEN}${MEM_MB}MB${NC} / ${THRESHOLD_MB}MB"
        fi
    else
        echo -e "${TIMESTAMP} | 无法获取内存信息: $MEM_RAW"
    fi
    
    sleep $CHECK_INTERVAL
done
