#!/bin/bash
set -e

# 1. 先设置目标架构
export GOOS=linux
export GOARCH=arm64

# 2. 【关键】立刻开启 CGO，并配置交叉编译环境
export CGO_ENABLED=1

# === 路径配置 ===
BR2_SDK_PATH=/home/harry/rock/atk_rkx_linux/buildroot/output/rockchip_rk3568/host
BR2_SYSROOT=${BR2_SDK_PATH}/aarch64-buildroot-linux-gnu/sysroot

# 【关键】指定 C 编译器 (Cross Compiler)
# Go 编译 C 代码时必须用这个，不能用宿主机的 gcc
export CC=${BR2_SDK_PATH}/bin/aarch64-buildroot-linux-gnu-gcc
export CXX=${BR2_SDK_PATH}/bin/aarch64-buildroot-linux-gnu-g++

# 【关键】设置头文件和库路径
export CGO_CFLAGS="--sysroot=${BR2_SYSROOT} -I${BR2_SYSROOT}/usr/include -I${BR2_SYSROOT}/usr/include/drm"
export CGO_LDFLAGS="--sysroot=${BR2_SYSROOT} -L${BR2_SYSROOT}/usr/lib -L${BR2_SYSROOT}/lib"
export PKG_CONFIG_PATH=${BR2_SYSROOT}/usr/lib/pkgconfig

# === 开始编译 ===
mkdir -p bin/arm64

echo "Building Executable (go_rtsp)..."
# 现在 CGO 环境已经准备好了，这步编译就能找到 utils 里的 C 代码了
go build -o bin/arm64/go_rtsp main.go

echo "Building Shared Library (libgortsp.so)..."
go build -buildmode=c-shared -o bin/arm64/libgortsp.so export.go

echo "Done."