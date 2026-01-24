#!/bin/bash

# Docker容器内存分析抓取脚本
# 用法: ./docker_memory_capture.sh <容器名或ID>

set -e

# 配置
CONTAINER="${1:-ant2api}"
OUTPUT_DIR="/tmp/docker_memory_$(date +%Y%m%d_%H%M%S)"
PPROF_PORT="${2:-6060}"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}=== Docker容器内存分析工具 ===${NC}"
echo ""

# 检查容器是否存在
if ! docker ps --format '{{.Names}}' | grep -q "^${CONTAINER}$" && \
   ! docker ps --format '{{.ID}}' | grep -q "^${CONTAINER}"; then
    # 尝试用部分匹配
    CONTAINER=$(docker ps --format '{{.Names}}' | grep -i "${CONTAINER}" | head -1)
    if [ -z "$CONTAINER" ]; then
        echo -e "${RED}错误: 找不到容器 $1${NC}"
        echo "运行中的容器:"
        docker ps --format "  {{.Names}} ({{.ID}})"
        exit 1
    fi
fi

echo -e "${GREEN}目标容器: ${CONTAINER}${NC}"
mkdir -p "$OUTPUT_DIR"
echo -e "${GREEN}输出目录: ${OUTPUT_DIR}${NC}"
echo ""

# 获取容器信息
echo -e "${YELLOW}[1/7] 获取容器基本信息...${NC}"
docker inspect "$CONTAINER" | jq '{
    Name: .[0].Name,
    Id: .[0].Id[0:12],
    Image: .[0].Config.Image,
    MemoryLimit: (.[0].HostConfig.Memory / 1024 / 1024 | tostring + " MB"),
    MemoryReservation: (.[0].HostConfig.MemoryReservation / 1024 / 1024 | tostring + " MB"),
    CPUs: .[0].HostConfig.NanoCpus,
    Pid: .[0].State.Pid,
    Status: .[0].State.Status,
    StartedAt: .[0].State.StartedAt
}' > "$OUTPUT_DIR/container_info.json" 2>/dev/null || docker inspect "$CONTAINER" > "$OUTPUT_DIR/container_info.json"
cat "$OUTPUT_DIR/container_info.json"
echo ""

# 获取容器PID
CONTAINER_PID=$(docker inspect --format '{{.State.Pid}}' "$CONTAINER")
echo -e "容器主进程PID: ${CONTAINER_PID}"
echo ""

# 获取实时内存统计
echo -e "${YELLOW}[2/7] 获取Docker内存统计...${NC}"
docker stats "$CONTAINER" --no-stream --format "table {{.Name}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.PIDs}}" | tee "$OUTPUT_DIR/docker_stats.txt"
echo ""

# 从cgroup获取详细内存信息
echo -e "${YELLOW}[3/7] 获取cgroup内存详情...${NC}"
CGROUP_PATH=""

# 尝试cgroups v2
if [ -f "/sys/fs/cgroup/system.slice/docker-$(docker inspect --format '{{.Id}}' $CONTAINER).scope/memory.current" ]; then
    CGROUP_PATH="/sys/fs/cgroup/system.slice/docker-$(docker inspect --format '{{.Id}}' $CONTAINER).scope"
    echo "检测到cgroups v2"
    echo "memory.current: $(cat $CGROUP_PATH/memory.current 2>/dev/null | awk '{printf "%.2f MB\n", $1/1024/1024}')" | tee -a "$OUTPUT_DIR/cgroup_memory.txt"
    echo "memory.max: $(cat $CGROUP_PATH/memory.max 2>/dev/null)" | tee -a "$OUTPUT_DIR/cgroup_memory.txt"
    cat $CGROUP_PATH/memory.stat 2>/dev/null > "$OUTPUT_DIR/cgroup_memory_stat.txt"
# 尝试cgroups v1
elif [ -d "/sys/fs/cgroup/memory/docker" ]; then
    CONTAINER_FULL_ID=$(docker inspect --format '{{.Id}}' "$CONTAINER")
    CGROUP_PATH="/sys/fs/cgroup/memory/docker/$CONTAINER_FULL_ID"
    if [ -f "$CGROUP_PATH/memory.usage_in_bytes" ]; then
        echo "检测到cgroups v1"
        echo "memory.usage: $(cat $CGROUP_PATH/memory.usage_in_bytes 2>/dev/null | awk '{printf "%.2f MB\n", $1/1024/1024}')" | tee -a "$OUTPUT_DIR/cgroup_memory.txt"
        echo "memory.limit: $(cat $CGROUP_PATH/memory.limit_in_bytes 2>/dev/null | awk '{printf "%.2f MB\n", $1/1024/1024}')" | tee -a "$OUTPUT_DIR/cgroup_memory.txt"
        cat $CGROUP_PATH/memory.stat 2>/dev/null > "$OUTPUT_DIR/cgroup_memory_stat.txt"
    fi
fi

# 如果无法直接访问cgroup，从容器内部获取
if [ ! -f "$OUTPUT_DIR/cgroup_memory.txt" ] || [ ! -s "$OUTPUT_DIR/cgroup_memory.txt" ]; then
    echo "从容器内部获取内存信息..."
    docker exec "$CONTAINER" sh -c '
        echo "=== /proc/meminfo ==="
        cat /proc/meminfo | head -20
        echo ""
        echo "=== cgroup memory ==="
        if [ -f /sys/fs/cgroup/memory.current ]; then
            echo "cgroups v2"
            echo "current: $(cat /sys/fs/cgroup/memory.current 2>/dev/null)"
            echo "max: $(cat /sys/fs/cgroup/memory.max 2>/dev/null)"
        elif [ -f /sys/fs/cgroup/memory/memory.usage_in_bytes ]; then
            echo "cgroups v1"
            echo "usage: $(cat /sys/fs/cgroup/memory/memory.usage_in_bytes 2>/dev/null)"
            echo "limit: $(cat /sys/fs/cgroup/memory/memory.limit_in_bytes 2>/dev/null)"
        fi
    ' 2>/dev/null > "$OUTPUT_DIR/cgroup_memory.txt" || echo "无法从容器内获取cgroup信息"
fi
echo ""

# 获取进程内存详情
echo -e "${YELLOW}[4/7] 获取进程内存详情 (从/proc)...${NC}"
if [ -d "/proc/$CONTAINER_PID" ]; then
    cat "/proc/$CONTAINER_PID/status" 2>/dev/null | grep -E "^(Name|Pid|VmSize|VmRSS|VmData|VmStk|VmLib|VmPTE|RssAnon|RssFile|RssShmem|Threads):" | tee "$OUTPUT_DIR/proc_status.txt"
    cat "/proc/$CONTAINER_PID/smaps_rollup" 2>/dev/null > "$OUTPUT_DIR/smaps_rollup.txt"
    echo ""
    echo "smaps_rollup 摘要:"
    cat "$OUTPUT_DIR/smaps_rollup.txt" | grep -E "^(Rss|Pss|Private|Shared|Anonymous):" | tee -a "$OUTPUT_DIR/proc_status.txt"
else
    echo "无法访问 /proc/$CONTAINER_PID，尝试从容器内获取..."
    docker exec "$CONTAINER" cat /proc/1/status 2>/dev/null | grep -E "^(Name|Pid|VmSize|VmRSS|VmData|RssAnon):" | tee "$OUTPUT_DIR/proc_status.txt"
fi
echo ""

# 获取pprof堆内存profile
echo -e "${YELLOW}[5/7] 抓取pprof堆内存profile...${NC}"

PPROF_SUCCESS=false

# 方法1: 检查端口映射（pprof端口映射到宿主机）
MAPPED_PORT=$(docker port "$CONTAINER" "$PPROF_PORT" 2>/dev/null | cut -d: -f2 | head -1)
if [ -n "$MAPPED_PORT" ]; then
    PPROF_URL="http://localhost:$MAPPED_PORT/debug/pprof"
    echo "尝试通过端口映射访问: $PPROF_URL"
    if curl -s --connect-timeout 2 "$PPROF_URL/heap" -o /dev/null 2>/dev/null; then
        echo "  端口映射可用，开始抓取..."
        curl -s "$PPROF_URL/heap" > "$OUTPUT_DIR/heap.pb.gz"
        curl -s "$PPROF_URL/allocs" > "$OUTPUT_DIR/allocs.pb.gz"
        curl -s "$PPROF_URL/goroutine" > "$OUTPUT_DIR/goroutine.pb.gz"
        curl -s "$PPROF_URL/heap?debug=1" > "$OUTPUT_DIR/heap_debug.txt"
        PPROF_SUCCESS=true
    fi
fi

# 方法2: 使用docker exec从容器内部抓取（推荐，因为pprof通常绑定127.0.0.1）
if [ "$PPROF_SUCCESS" = false ]; then
    echo "通过docker exec从容器内部抓取pprof..."
    
    # 检查容器内是否能访问pprof
    if docker exec "$CONTAINER" sh -c "wget -q -O /dev/null http://127.0.0.1:$PPROF_PORT/debug/pprof/ 2>/dev/null" 2>/dev/null; then
        echo "  pprof在容器内可访问，使用wget抓取..."
        
        docker exec "$CONTAINER" wget -q -O - "http://127.0.0.1:$PPROF_PORT/debug/pprof/heap" > "$OUTPUT_DIR/heap.pb.gz" 2>/dev/null
        docker exec "$CONTAINER" wget -q -O - "http://127.0.0.1:$PPROF_PORT/debug/pprof/allocs" > "$OUTPUT_DIR/allocs.pb.gz" 2>/dev/null
        docker exec "$CONTAINER" wget -q -O - "http://127.0.0.1:$PPROF_PORT/debug/pprof/goroutine" > "$OUTPUT_DIR/goroutine.pb.gz" 2>/dev/null
        docker exec "$CONTAINER" wget -q -O - "http://127.0.0.1:$PPROF_PORT/debug/pprof/heap?debug=1" > "$OUTPUT_DIR/heap_debug.txt" 2>/dev/null
        
        if [ -s "$OUTPUT_DIR/heap.pb.gz" ]; then
            PPROF_SUCCESS=true
            echo -e "  ${GREEN}成功从容器内部抓取pprof profiles${NC}"
        fi
    elif docker exec "$CONTAINER" sh -c "curl -s http://127.0.0.1:$PPROF_PORT/debug/pprof/ >/dev/null 2>&1" 2>/dev/null; then
        echo "  pprof在容器内可访问，使用curl抓取..."
        
        docker exec "$CONTAINER" curl -s "http://127.0.0.1:$PPROF_PORT/debug/pprof/heap" > "$OUTPUT_DIR/heap.pb.gz" 2>/dev/null
        docker exec "$CONTAINER" curl -s "http://127.0.0.1:$PPROF_PORT/debug/pprof/allocs" > "$OUTPUT_DIR/allocs.pb.gz" 2>/dev/null
        docker exec "$CONTAINER" curl -s "http://127.0.0.1:$PPROF_PORT/debug/pprof/goroutine" > "$OUTPUT_DIR/goroutine.pb.gz" 2>/dev/null
        docker exec "$CONTAINER" curl -s "http://127.0.0.1:$PPROF_PORT/debug/pprof/heap?debug=1" > "$OUTPUT_DIR/heap_debug.txt" 2>/dev/null
        
        if [ -s "$OUTPUT_DIR/heap.pb.gz" ]; then
            PPROF_SUCCESS=true
            echo -e "  ${GREEN}成功从容器内部抓取pprof profiles${NC}"
        fi
    fi
fi

# 方法3: 尝试通过容器IP访问（如果pprof绑定0.0.0.0）
if [ "$PPROF_SUCCESS" = false ]; then
    CONTAINER_IP=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$CONTAINER")
    if [ -n "$CONTAINER_IP" ]; then
        PPROF_URL="http://$CONTAINER_IP:$PPROF_PORT/debug/pprof"
        echo "尝试通过容器IP访问: $PPROF_URL"
        if curl -s --connect-timeout 2 "$PPROF_URL/heap" -o /dev/null 2>/dev/null; then
            echo "  容器IP可访问，开始抓取..."
            curl -s "$PPROF_URL/heap" > "$OUTPUT_DIR/heap.pb.gz"
            curl -s "$PPROF_URL/allocs" > "$OUTPUT_DIR/allocs.pb.gz"
            curl -s "$PPROF_URL/goroutine" > "$OUTPUT_DIR/goroutine.pb.gz"
            curl -s "$PPROF_URL/heap?debug=1" > "$OUTPUT_DIR/heap_debug.txt"
            PPROF_SUCCESS=true
        fi
    fi
fi

if [ "$PPROF_SUCCESS" = true ]; then
    echo -e "${GREEN}pprof profiles已保存${NC}"
else
    echo -e "${RED}警告: 无法访问pprof端点${NC}"
    echo "请确保:"
    echo "  1. 容器中启用了pprof (import _ \"net/http/pprof\")"
    echo "  2. pprof正在监听 (检查: docker exec $CONTAINER netstat -tlnp)"
    echo "  3. 容器内有wget或curl工具"
fi
echo ""

# 分析pprof结果
echo -e "${YELLOW}[6/7] 分析pprof结果...${NC}"
if [ -f "$OUTPUT_DIR/heap.pb.gz" ] && [ -s "$OUTPUT_DIR/heap.pb.gz" ]; then
    echo "=== 堆内存热点 (inuse_space) ===" | tee "$OUTPUT_DIR/analysis.txt"
    go tool pprof -top 30 "$OUTPUT_DIR/heap.pb.gz" 2>/dev/null | tee -a "$OUTPUT_DIR/analysis.txt" || echo "无法分析heap profile"
    echo "" | tee -a "$OUTPUT_DIR/analysis.txt"
    
    if [ -f "$OUTPUT_DIR/allocs.pb.gz" ] && [ -s "$OUTPUT_DIR/allocs.pb.gz" ]; then
        echo "=== 总分配量热点 (alloc_space) ===" | tee -a "$OUTPUT_DIR/analysis.txt"
        go tool pprof -alloc_space -top 30 "$OUTPUT_DIR/allocs.pb.gz" 2>/dev/null | tee -a "$OUTPUT_DIR/analysis.txt" || echo "无法分析allocs profile"
    fi
else
    echo "没有pprof数据可分析"
fi
echo ""

# 获取Go runtime内存统计
echo -e "${YELLOW}[7/7] 提取Go runtime内存统计...${NC}"
if [ -f "$OUTPUT_DIR/heap_debug.txt" ] && [ -s "$OUTPUT_DIR/heap_debug.txt" ]; then
    # 从heap debug输出中提取统计信息
    head -3 "$OUTPUT_DIR/heap_debug.txt" | tee "$OUTPUT_DIR/memstats_summary.txt"
fi
echo ""

# 生成报告
echo -e "${BLUE}=== 生成分析报告 ===${NC}"
cat > "$OUTPUT_DIR/REPORT.md" << EOF
# Docker容器内存分析报告

**容器**: $CONTAINER
**时间**: $(date)
**输出目录**: $OUTPUT_DIR

## 快速摘要

### Docker统计
\`\`\`
$(cat "$OUTPUT_DIR/docker_stats.txt")
\`\`\`

### 进程内存
\`\`\`
$(cat "$OUTPUT_DIR/proc_status.txt" 2>/dev/null || echo "N/A")
\`\`\`

## 文件列表

- container_info.json - 容器配置信息
- docker_stats.txt - Docker实时统计
- cgroup_memory.txt - cgroup内存信息
- proc_status.txt - /proc/PID/status
- smaps_rollup.txt - 详细地址空间内存
- heap.pb.gz - pprof堆内存profile
- allocs.pb.gz - pprof分配量profile
- goroutine.pb.gz - goroutine profile
- heap_debug.txt - pprof debug输出
- analysis.txt - 热点分析结果

## 分析命令

\`\`\`bash
# 查看堆内存热点
go tool pprof -top 50 $OUTPUT_DIR/heap.pb.gz

# 查看总分配量
go tool pprof -alloc_space -top 50 $OUTPUT_DIR/allocs.pb.gz

# 交互式分析
go tool pprof $OUTPUT_DIR/heap.pb.gz

# 生成火焰图 (需要安装graphviz)
go tool pprof -svg $OUTPUT_DIR/heap.pb.gz > $OUTPUT_DIR/heap.svg

# Web界面分析
go tool pprof -http=:8080 $OUTPUT_DIR/heap.pb.gz
\`\`\`

EOF

echo -e "${GREEN}=== 抓取完成 ===${NC}"
echo ""
echo "输出目录: $OUTPUT_DIR"
echo ""
echo "快速分析命令:"
echo "  go tool pprof -top 50 $OUTPUT_DIR/heap.pb.gz"
echo "  go tool pprof -web $OUTPUT_DIR/heap.pb.gz  (需要浏览器)"
echo ""
echo "查看报告:"
echo "  cat $OUTPUT_DIR/REPORT.md"
echo ""
