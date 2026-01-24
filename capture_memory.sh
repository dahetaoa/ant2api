#!/bin/bash

# 内存分析抓取脚本
# 用法: ./capture_memory.sh

PPROF_URL="http://localhost:6060/debug/pprof"
OUTPUT_DIR="/tmp/memory_analysis_$(date +%Y%m%d_%H%M%S)"
mkdir -p "$OUTPUT_DIR"

echo "=== 内存分析抓取脚本 ==="
echo "输出目录: $OUTPUT_DIR"
echo ""

# 1. 抓取请求前的基线
echo "[1/4] 抓取基线内存..."
curl -s "$PPROF_URL/heap" > "$OUTPUT_DIR/1_baseline.pb.gz"
echo "  基线已保存"

# 2. 等待用户发送请求
echo ""
echo "=========================================="
echo "请现在发送包含图片的 API 请求！"
echo "在请求发送后，按 Enter 键继续抓取..."
echo "=========================================="
read -r

# 3. 立即抓取（请求处理中）
echo ""
echo "[2/4] 抓取请求处理中的内存..."
curl -s "$PPROF_URL/heap" > "$OUTPUT_DIR/2_during_request.pb.gz"
echo "  已保存"

# 4. 再抓一次（可能请求还在处理）
sleep 0.5
echo "[3/4] 延迟 0.5s 再次抓取..."
curl -s "$PPROF_URL/heap" > "$OUTPUT_DIR/3_during_request_2.pb.gz"
echo "  已保存"

# 5. 等待请求完成
echo ""
echo "等待请求完成后按 Enter..."
read -r

# 6. 抓取请求完成后
echo "[4/4] 抓取请求完成后的内存..."
curl -s "$PPROF_URL/heap" > "$OUTPUT_DIR/4_after_request.pb.gz"
echo "  已保存"

echo ""
echo "=== 抓取完成 ==="
echo ""
echo "分析命令:"
echo ""
echo "# 查看请求处理时的内存热点 (inuse):"
echo "go tool pprof -top 30 $OUTPUT_DIR/2_during_request.pb.gz"
echo ""
echo "# 查看总分配量 (alloc):"
echo "go tool pprof -alloc_space -top 30 $OUTPUT_DIR/2_during_request.pb.gz"
echo ""
echo "# 对比基线和请求时的差异:"
echo "go tool pprof -top 30 -base=$OUTPUT_DIR/1_baseline.pb.gz $OUTPUT_DIR/2_during_request.pb.gz"
echo ""
echo "# 对比请求时和完成后的差异 (查找内存泄漏):"
echo "go tool pprof -top 30 -base=$OUTPUT_DIR/4_after_request.pb.gz $OUTPUT_DIR/2_during_request.pb.gz"
echo ""
