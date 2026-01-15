#!/bin/bash
#
# Antigravity 2 API - 启动脚本
# 功能：检测环境变量、检查代码更新、构建并启动服务
#

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 配置
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_BIN="$SCRIPT_DIR/server"
BUILD_HASH_FILE="$SCRIPT_DIR/.build_hash"
TEMPL_VIEWS_DIR="$SCRIPT_DIR/internal/gateway/manager/views"

# 清屏
clear

echo -e "${CYAN}╔══════════════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║       ${GREEN}Antigravity 2 API${CYAN} - 启动脚本                       ║${NC}"
echo -e "${CYAN}╚══════════════════════════════════════════════════════════╝${NC}"
echo ""

# =============================================================================
# 函数定义
# =============================================================================

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[✓]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[!]${NC} $1"
}

log_error() {
    echo -e "${RED}[✗]${NC} $1"
}

# 加载 .env 文件
load_env() {
    if [[ -f "$SCRIPT_DIR/.env" ]]; then
        set -a
        source "$SCRIPT_DIR/.env"
        set +a
        log_success "已加载 .env 配置文件"
    else
        log_warn ".env 文件不存在，将使用默认配置或环境变量"
    fi
}

# 检查必要的环境变量
check_required_vars() {
    local missing_vars=()
    local var_descriptions=(
        "ADMIN_PASSWORD:管理面板登录密码"
    )
    
    # 定义推荐配置的变量（非必须但推荐设置）
    local recommended_vars=(
        "API_KEY:API 访问密钥，用于保护 API 端点，防止未授权访问"
    )

    echo -e "\n${CYAN}━━━ 环境变量检查 ━━━${NC}\n"

    # 检查必须的变量
    for item in "${var_descriptions[@]}"; do
        var_name="${item%%:*}"
        var_desc="${item#*:}"
        
        if [[ -z "${!var_name}" ]]; then
            missing_vars+=("$item")
        else
            log_success "$var_name 已设置"
        fi
    done

    # 提示推荐变量
    local unset_recommended=()
    for item in "${recommended_vars[@]}"; do
        var_name="${item%%:*}"
        var_desc="${item#*:}"
        
        if [[ -z "${!var_name}" ]]; then
            unset_recommended+=("$item")
        else
            log_success "$var_name 已设置"
        fi
    done

    # 如果有必须变量未设置
    if [[ ${#missing_vars[@]} -gt 0 ]]; then
        echo ""
        log_warn "以下必须的环境变量尚未设置："
        echo ""
        for item in "${missing_vars[@]}"; do
            var_name="${item%%:*}"
            var_desc="${item#*:}"
            echo -e "  ${YELLOW}$var_name${NC}"
            echo -e "    └─ $var_desc"
        done
        echo ""
        
        read -p "是否现在设置这些变量？(y/N): " setup_choice
        if [[ "$setup_choice" =~ ^[Yy]$ ]]; then
            setup_missing_vars "${missing_vars[@]}"
        else
            log_error "必须的变量未设置，无法启动"
            exit 1
        fi
    fi

    # 提示推荐变量
    if [[ ${#unset_recommended[@]} -gt 0 ]]; then
        echo ""
        log_warn "以下推荐的环境变量尚未设置（可选）："
        echo ""
        for item in "${unset_recommended[@]}"; do
            var_name="${item%%:*}"
            var_desc="${item#*:}"
            echo -e "  ${YELLOW}$var_name${NC}"
            echo -e "    └─ $var_desc"
        done
        echo ""
        
        read -p "是否现在设置这些变量？(y/N): " setup_choice
        if [[ "$setup_choice" =~ ^[Yy]$ ]]; then
            setup_missing_vars "${unset_recommended[@]}"
        fi
    fi
}

# 设置缺失的变量
setup_missing_vars() {
    local vars=("$@")
    local env_updates=""
    
    for item in "${vars[@]}"; do
        var_name="${item%%:*}"
        var_desc="${item#*:}"
        
        echo ""
        echo -e "${CYAN}设置 $var_name${NC}"
        echo -e "  说明: $var_desc"
        
        # 根据变量类型给出特定提示
        case "$var_name" in
            ADMIN_PASSWORD)
                echo -e "  ${YELLOW}提示: 建议使用强密码，至少8个字符${NC}"
                read -sp "  请输入 $var_name: " var_value
                echo ""
                ;;
            API_KEY)
                echo -e "  ${YELLOW}提示: 格式如 sk-xxxxx，留空则禁用 API 密钥验证${NC}"
                read -p "  请输入 $var_name (可留空): " var_value
                ;;

            *)
                read -p "  请输入 $var_name: " var_value
                ;;
        esac
        
        if [[ -n "$var_value" ]]; then
            export "$var_name=$var_value"
            env_updates+="$var_name=$var_value\n"
            log_success "$var_name 已设置"
        else
            log_warn "$var_name 保持为空"
        fi
    done
    
    # 询问是否保存到 .env 文件
    if [[ -n "$env_updates" ]]; then
        echo ""
        read -p "是否将这些设置保存到 .env 文件？(Y/n): " save_choice
        if [[ ! "$save_choice" =~ ^[Nn]$ ]]; then
            echo -e "$env_updates" >> "$SCRIPT_DIR/.env"
            log_success "已保存到 .env 文件"
        fi
    fi
}

# 计算源代码哈希
calculate_source_hash() {
    # 计算 Go 源文件和 templ 文件的哈希
    find "$SCRIPT_DIR" -type f \( -name "*.go" -o -name "*.templ" \) \
        -not -path "*/.git/*" \
        -not -path "*/vendor/*" \
        -not -name "*_templ.go" \
        -exec md5sum {} \; 2>/dev/null | sort | md5sum | cut -d' ' -f1
}

# 获取上次构建的哈希
get_last_build_hash() {
    if [[ -f "$BUILD_HASH_FILE" ]]; then
        cat "$BUILD_HASH_FILE"
    else
        echo ""
    fi
}

# 保存构建哈希
save_build_hash() {
    echo "$1" > "$BUILD_HASH_FILE"
}

# 检查是否需要重新构建
needs_rebuild() {
    local current_hash
    current_hash=$(calculate_source_hash)
    local last_hash
    last_hash=$(get_last_build_hash)
    
    # 如果二进制文件不存在，需要构建
    if [[ ! -f "$SERVER_BIN" ]]; then
        return 0
    fi
    
    # 如果哈希不同，需要构建
    if [[ "$current_hash" != "$last_hash" ]]; then
        return 0
    fi
    
    return 1
}

# 构建前端（templ）
build_frontend() {
    log_info "正在构建前端模板..."
    
    # 检查 templ 是否安装
    if ! command -v templ &> /dev/null; then
        # 尝试使用 ~/go/bin/templ
        if [[ -x "$HOME/go/bin/templ" ]]; then
            TEMPL_CMD="$HOME/go/bin/templ"
        else
            log_warn "templ 未安装，正在安装..."
            go install github.com/a-h/templ/cmd/templ@latest
            TEMPL_CMD="$HOME/go/bin/templ"
        fi
    else
        TEMPL_CMD="templ"
    fi
    
    if $TEMPL_CMD generate "$TEMPL_VIEWS_DIR" 2>&1; then
        log_success "前端模板构建完成"
    else
        log_error "前端模板构建失败"
        exit 1
    fi
}

# 构建后端
build_backend() {
    log_info "正在构建后端..."
    
    cd "$SCRIPT_DIR"
    if go build -o "$SERVER_BIN" ./cmd/server 2>&1; then
        log_success "后端构建完成"
    else
        log_error "后端构建失败"
        exit 1
    fi
}

# 构建项目
build_project() {
    echo -e "\n${CYAN}━━━ 构建项目 ━━━${NC}\n"
    
    build_frontend
    build_backend
    
    # 保存构建哈希
    local current_hash
    current_hash=$(calculate_source_hash)
    save_build_hash "$current_hash"
    
    log_success "项目构建完成"
}

# 停止已运行的服务
stop_existing_server() {
    if pgrep -f "$SERVER_BIN" > /dev/null 2>&1; then
        log_info "正在停止已运行的服务..."
        pkill -f "$SERVER_BIN" 2>/dev/null || true
        sleep 1
        log_success "已停止旧服务"
    fi
}

# 启动服务
start_server() {
    echo -e "\n${CYAN}━━━ 启动服务 ━━━${NC}\n"
    
    stop_existing_server
    
    local port="${PORT:-8045}"
    local host="${HOST:-0.0.0.0}"
    
    log_info "启动服务器: http://$host:$port"
    echo ""
    
    cd "$SCRIPT_DIR"
    exec "$SERVER_BIN"
}

# =============================================================================
# 主流程
# =============================================================================

main() {
    cd "$SCRIPT_DIR"
    
    # 加载环境变量
    load_env
    
    # 检查必要变量
    check_required_vars
    
    echo -e "\n${CYAN}━━━ 代码检查 ━━━${NC}\n"
    
    # 检查是否需要重新构建
    if needs_rebuild; then
        log_info "检测到代码更新，需要重新构建"
        build_project
    else
        log_success "代码无更新，跳过构建"
    fi
    
    # 启动服务
    start_server
}

# 运行主程序
main "$@"
