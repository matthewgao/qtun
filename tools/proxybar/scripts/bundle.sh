#!/bin/bash
# 组装 ProxyBar.app：release 构建 → bundle → ad-hoc 签名。
# UNIVERSAL=1 时构建 x86_64 + arm64 通用二进制。
# 不用 App Sandbox（会挡 networksetup 子进程），无 entitlements。
set -euo pipefail
cd "$(dirname "$0")/.."

ARCH_FLAGS=""
if [[ "${UNIVERSAL:-0}" == "1" ]]; then
    ARCH_FLAGS="--arch x86_64 --arch arm64"
fi

# shellcheck disable=SC2086
swift build -c release $ARCH_FLAGS
# shellcheck disable=SC2086
BIN_PATH=$(swift build -c release $ARCH_FLAGS --show-bin-path)

APP=build/ProxyBar.app
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BIN_PATH/ProxyBar" "$APP/Contents/MacOS/ProxyBar"
cp Resources/Info.plist "$APP/Contents/Info.plist"

codesign --force --sign - "$APP"
echo "Built $APP"