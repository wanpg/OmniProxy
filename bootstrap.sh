#!/usr/bin/env bash
#
# OmniProxy — 一键安装脚本
# 用法: curl -fsSL https://raw.githubusercontent.com/wanpg/OmniProxy/main/bootstrap.sh | bash
# 或:  ./bootstrap.sh [install|update|docker|uninstall]
#
set -euo pipefail

REPO="wanpg/OmniProxy"
BINARY="omniproxy"
INSTALL_DIR="/opt/omniproxy"
CONFIG_FILE="config.yaml"
SERVICE_NAME="omniproxy"

# ── 颜色 ──
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info()  { echo -e "${BLUE}[INFO]${NC} $*"; }
ok()    { echo -e "${GREEN}[OK]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
fail()  { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

# ── 检查依赖 ──
check_deps() {
    local missing=()

    for cmd in git go; do
        if ! command -v "$cmd" &>/dev/null; then
            missing+=("$cmd")
        fi
    done

    if [[ ${#missing[@]} -gt 0 ]]; then
        fail "缺少依赖: ${missing[*]}\n请先安装: sudo apt install ${missing[*]}"
    fi

    # 检查 CGO (SQLite 需要)
    if ! command -v gcc &>/dev/null; then
        warn "未找到 gcc，SQLite 可能无法编译"
        warn "安装: sudo apt install gcc libsqlite3-dev"
    fi
}

# ── 安装 ──
do_install() {
    info "🚀 开始安装 OmniProxy..."

    check_deps

    # 克隆或更新
    if [[ -d "$INSTALL_DIR/.git" ]]; then
        info "更新代码..."
        git -C "$INSTALL_DIR" pull --ff-only
    else
        info "克隆仓库..."
        git clone "https://github.com/${REPO}.git" "$INSTALL_DIR"
    fi

    # 编译
    info "编译中..."
    cd "$INSTALL_DIR"
    CGO_ENABLED=1 go build -o "$BINARY" .
    ok "编译成功: $INSTALL_DIR/$BINARY"

    # 配置文件
    if [[ ! -f "$INSTALL_DIR/$CONFIG_FILE" ]]; then
        if [[ -f "$INSTALL_DIR/config.yaml.example" ]]; then
            cp "$INSTALL_DIR/config.yaml.example" "$INSTALL_DIR/$CONFIG_FILE"
            ok "已生成默认配置: $INSTALL_DIR/$CONFIG_FILE"
            warn "请编辑配置文件，填入你的 API Key 后启动服务"
        else
            warn "未找到 config.yaml.example，请手动创建配置"
        fi
    else
        ok "配置文件已存在，跳过"
    fi

    # systemd 服务
    setup_systemd

    echo ""
    ok "安装完成！"
    echo ""
    echo -e "  ${BLUE}配置文件:${NC}  $INSTALL_DIR/$CONFIG_FILE"
    echo -e "  ${BLUE}启动服务:${NC}  sudo systemctl start $SERVICE_NAME"
    echo -e "  ${BLUE}查看状态:${NC}  sudo systemctl status $SERVICE_NAME"
    echo -e "  ${BLUE}查看日志:${NC}  sudo journalctl -u $SERVICE_NAME -f"
    echo -e "  ${BLUE}管理页面:${NC}  http://localhost:8080/admin/ui"
    echo ""
}

# ── 更新（仅编译+重启） ──
do_update() {
    info "🔄 更新 OmniProxy..."
    cd "$INSTALL_DIR"
    git pull --ff-only
    CGO_ENABLED=1 go build -o "$BINARY" .
    ok "编译成功"
    if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
        sudo systemctl restart "$SERVICE_NAME"
        ok "服务已重启"
    else
        warn "服务未运行，跳过重启"
    fi
}

# ── Docker 部署 ──
do_docker() {
    info "🐳 Docker 部署..."

    if ! command -v docker &>/dev/null; then
        fail "未找到 docker，请先安装 Docker"
    fi

    # 克隆或更新
    if [[ -d "$INSTALL_DIR/.git" ]]; then
        git -C "$INSTALL_DIR" pull --ff-only
    else
        git clone "https://github.com/${REPO}.git" "$INSTALL_DIR"
    fi

    cd "$INSTALL_DIR"

    # 构建镜像
    # 先本地编译 CGO 二进制
    info "编译 CGO 二进制..."
    CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o gateway-proxy-cgo .
    ok "编译成功"

    # 构建镜像
    info "构建 Docker 镜像..."
    docker build -t omniproxy:latest .

    # 停止旧容器
    docker rm -f gateway-proxy 2>/dev/null || true

    # 启动
    info "启动容器..."
    docker run -d \
        --name gateway-proxy \
        --restart always \
        -p 8080:8080 \
        -v "$INSTALL_DIR/$CONFIG_FILE:/config.yaml:ro" \
        omniproxy:latest

    ok "Docker 部署完成！"
    echo ""
    echo -e "  ${BLUE}管理页面:${NC}  http://localhost:8080/admin/ui"
    echo -e "  ${BLUE}查看日志:${NC}  docker logs -f gateway-proxy"
    echo ""
}

# ── 卸载 ──
do_uninstall() {
    warn "将删除 OmniProxy（配置文件保留）"
    read -rp "确认？[y/N] " confirm
    [[ "$confirm" != "y" && "$confirm" != "Y" ]] && exit 0

    sudo systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    sudo systemctl disable "$SERVICE_NAME" 2>/dev/null || true
    sudo rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
    sudo systemctl daemon-reload

    docker rm -f gateway-proxy 2>/dev/null || true
    docker rmi omniproxy:latest 2>/dev/null || true

    rm -rf "$INSTALL_DIR"
    ok "已卸载"
}

# ── systemd 服务 ──
setup_systemd() {
    if ! command -v systemctl &>/dev/null; then
        warn "未找到 systemctl，跳过服务注册（手动运行: $INSTALL_DIR/$BINARY）"
        return
    fi

    info "注册 systemd 服务..."
    sudo tee "/etc/systemd/system/${SERVICE_NAME}.service" > /dev/null <<EOF
[Unit]
Description=OmniProxy - Multi-provider LLM Gateway
After=network.target

[Service]
Type=simple
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/$BINARY
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

    sudo systemctl daemon-reload
    sudo systemctl enable "$SERVICE_NAME"
    ok "服务已注册（未自动启动，请先编辑配置）"
}

# ── 主入口 ──
case "${1:-install}" in
    install)   do_install ;;
    update)    do_update ;;
    docker)    do_docker ;;
    uninstall) do_uninstall ;;
    *)
        echo "OmniProxy 安装脚本"
        echo ""
        echo "用法: $0 <command>"
        echo ""
        echo "Commands:"
        echo "  install    安装（默认）— 克隆、编译、注册 systemd 服务"
        echo "  update     更新代码并重启服务"
        echo "  docker     Docker 部署"
        echo "  uninstall  卸载"
        ;;
esac
