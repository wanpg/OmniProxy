#!/usr/bin/env bash
#
# OmniProxy — 一键安装脚本（支持 Linux & macOS）
# 用法: curl -fsSL https://raw.githubusercontent.com/wanpg/OmniProxy/main/bootstrap.sh | bash
# 或:  ./bootstrap.sh [install|update|docker|uninstall]
#
set -euo pipefail

REPO="wanpg/OmniProxy"
BINARY="omniproxy"
SERVICE_NAME="omniproxy"

# ── 平台检测 ──
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
    Linux*)  PLATFORM="linux" ;;
    Darwin*) PLATFORM="macos" ;;
    *)       echo "不支持的系统: $OS"; exit 1 ;;
esac

case "$ARCH" in
    x86_64|amd64) GOARCH="amd64" ;;
    aarch64|arm64) GOARCH="arm64" ;;
    *) echo "不支持的架构: $ARCH"; exit 1 ;;
esac

# 安装目录
if [[ "$PLATFORM" == "macos" ]]; then
    INSTALL_DIR="${HOME}/.local/share/omniproxy"
else
    INSTALL_DIR="/opt/omniproxy"
fi

CONFIG_FILE="config.yaml"

# ── 颜色 ──
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info()  { echo -e "${BLUE}[INFO]${NC} $*"; }
ok()    { echo -e "${GREEN}[OK]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
fail()  { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

# ── 检查依赖 ──
check_deps() {
    local missing=()

    if ! command -v git &>/dev/null; then
        missing+=("git")
    fi
    if ! command -v go &>/dev/null; then
        missing+=("go")
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        if [[ "$PLATFORM" == "macos" ]]; then
            fail "缺少依赖: ${missing[*]}\n安装: brew install ${missing[*]}"
        else
            fail "缺少依赖: ${missing[*]}\n安装: sudo apt install ${missing[*]}"
        fi
    fi

    # CGO
    if ! command -v gcc &>/dev/null; then
        if [[ "$PLATFORM" == "macos" ]]; then
            warn "未找到 gcc，安装: xcode-select --install"
        else
            warn "未找到 gcc，安装: sudo apt install gcc libsqlite3-dev"
        fi
    fi
}

# ── 安装 ──
do_install() {
    info "🚀 安装 OmniProxy ($OS/$GOARCH)..."

    check_deps

    # 克隆或更新
    if [[ -d "$INSTALL_DIR/.git" ]]; then
        info "更新代码..."
        git -C "$INSTALL_DIR" pull --ff-only
    else
        info "克隆仓库 → $INSTALL_DIR"
        mkdir -p "$(dirname "$INSTALL_DIR")"
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
        fi
    else
        ok "配置文件已存在，跳过"
    fi

    # 服务注册
    if [[ "$PLATFORM" == "macos" ]]; then
        setup_launchd
    else
        setup_systemd
    fi

    # 创建便捷 symlink（macOS）
    if [[ "$PLATFORM" == "macos" ]]; then
        local bin_dir="${HOME}/.local/bin"
        mkdir -p "$bin_dir"
        ln -sf "$INSTALL_DIR/$BINARY" "$bin_dir/$BINARY"
        ok "可执行文件链接: $bin_dir/$BINARY"
        if ! echo "$PATH" | grep -q "$bin_dir"; then
            warn "请确保 ~/.local/bin 在 PATH 中"
            warn "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc"
        fi
    fi

    echo ""
    ok "安装完成！"
    echo ""
    echo -e "  ${BLUE}配置文件:${NC}  $INSTALL_DIR/$CONFIG_FILE"
    if [[ "$PLATFORM" == "macos" ]]; then
        echo -e "  ${BLUE}启动服务:${NC}  launchctl load $HOME/Library/LaunchAgents/com.omniproxy.plist"
        echo -e "  ${BLUE}查看日志:${NC}  tail -f /tmp/omniproxy.log"
    else
        echo -e "  ${BLUE}启动服务:${NC}  sudo systemctl start $SERVICE_NAME"
        echo -e "  ${BLUE}查看状态:${NC}  sudo systemctl status $SERVICE_NAME"
        echo -e "  ${BLUE}查看日志:${NC}  sudo journalctl -u $SERVICE_NAME -f"
    fi
    echo -e "  ${BLUE}管理页面:${NC}  http://localhost:8080/admin/ui"
    echo ""
}

# ── 更新 ──
do_update() {
    info "🔄 更新 OmniProxy..."
    cd "$INSTALL_DIR"
    git pull --ff-only
    CGO_ENABLED=1 go build -o "$BINARY" .
    ok "编译成功"

    if [[ "$PLATFORM" == "macos" ]]; then
        if launchctl list "$SERVICE_NAME" &>/dev/null 2>&1; then
            launchctl kickstart -k "gui/$(id -u)/$SERVICE_NAME"
            ok "服务已重启"
        else
            warn "服务未运行，跳过重启"
        fi
    else
        if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
            sudo systemctl restart "$SERVICE_NAME"
            ok "服务已重启"
        else
            warn "服务未运行，跳过重启"
        fi
    fi
}

# ── Docker 部署 ──
do_docker() {
    info "🐳 Docker 部署..."

    if ! command -v docker &>/dev/null; then
        fail "未找到 docker，请先安装 Docker"
    fi

    if [[ -d "$INSTALL_DIR/.git" ]]; then
        git -C "$INSTALL_DIR" pull --ff-only
    else
        mkdir -p "$(dirname "$INSTALL_DIR")"
        git clone "https://github.com/${REPO}.git" "$INSTALL_DIR"
    fi

    cd "$INSTALL_DIR"

    info "编译 CGO 二进制..."
    CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o gateway-proxy-cgo .
    ok "编译成功"

    info "构建 Docker 镜像..."
    docker build -t omniproxy:latest .

    docker rm -f gateway-proxy 2>/dev/null || true

    info "启动容器..."
    docker run -d \
        --name gateway-proxy \
        --restart always \
        -p 8080:8080 \
        -v "$INSTALL_DIR/$CONFIG_FILE:/config.yaml:ro" \
        omniproxy:latest

    ok "Docker 部署完成！"
    echo -e "  ${BLUE}管理页面:${NC}  http://localhost:8080/admin/ui"
    echo -e "  ${BLUE}查看日志:${NC}  docker logs -f gateway-proxy"
}

# ── 卸载 ──
do_uninstall() {
    warn "将删除 OmniProxy（配置文件保留）"
    read -rp "确认？[y/N] " confirm
    [[ "$confirm" != "y" && "$confirm" != "Y" ]] && exit 0

    if [[ "$PLATFORM" == "macos" ]]; then
        launchctl unload "$HOME/Library/LaunchAgents/com.omniproxy.plist" 2>/dev/null || true
        rm -f "$HOME/Library/LaunchAgents/com.omniproxy.plist"
        rm -f "${HOME}/.local/bin/$BINARY"
        rm -f /tmp/omniproxy.log
    else
        sudo systemctl stop "$SERVICE_NAME" 2>/dev/null || true
        sudo systemctl disable "$SERVICE_NAME" 2>/dev/null || true
        sudo rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
        sudo systemctl daemon-reload
    fi

    docker rm -f gateway-proxy 2>/dev/null || true
    docker rmi omniproxy:latest 2>/dev/null || true

    rm -rf "$INSTALL_DIR"
    ok "已卸载"
}

# ── systemd 服务 (Linux) ──
setup_systemd() {
    if ! command -v systemctl &>/dev/null; then
        warn "未找到 systemctl，手动运行: $INSTALL_DIR/$BINARY"
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

# ── launchd 服务 (macOS) ──
setup_launchd() {
    local plist_dir="$HOME/Library/LaunchAgents"
    local plist_file="$plist_dir/com.omniproxy.plist"

    mkdir -p "$plist_dir"

    info "注册 launchd 服务..."
    cat > "$plist_file" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.omniproxy</string>
    <key>ProgramArguments</key>
    <array>
        <string>$INSTALL_DIR/$BINARY</string>
    </array>
    <key>WorkingDirectory</key>
    <string>$INSTALL_DIR</string>
    <key>RunAtLoad</key>
    <false/>
    <key>KeepAlive</key>
    <false/>
    <key>StandardOutPath</key>
    <string>/tmp/omniproxy.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/omniproxy.log</string>
</dict>
</plist>
EOF

    ok "服务已注册（未自动启动，请先编辑配置）"
}

# ── 主入口 ──
case "${1:-install}" in
    install)   do_install ;;
    update)    do_update ;;
    docker)    do_docker ;;
    uninstall) do_uninstall ;;
    *)
        echo "OmniProxy 安装脚本 ($OS/$GOARCH)"
        echo ""
        echo "用法: $0 <command>"
        echo ""
        echo "Commands:"
        echo "  install    安装（默认）— 克隆、编译、注册系统服务"
        echo "  update     更新代码并重启服务"
        echo "  docker     Docker 部署"
        echo "  uninstall  卸载"
        ;;
esac
