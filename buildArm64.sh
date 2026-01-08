#!/bin/bash
set -e

export GOOS=linux
export GOARCH=arm64

export CGO_ENABLED=1

# === 路径配置 ===
BR2_SDK_PATH=/home/harry/rock/atk_rkx_linux/buildroot/output/rockchip_rk3568/host
BR2_SYSROOT=${BR2_SDK_PATH}/aarch64-buildroot-linux-gnu/sysroot

export CC=${BR2_SDK_PATH}/bin/aarch64-buildroot-linux-gnu-gcc
export CXX=${BR2_SDK_PATH}/bin/aarch64-buildroot-linux-gnu-g++

# 设置头文件和库路径
export CGO_CFLAGS="--sysroot=${BR2_SYSROOT} -I${BR2_SYSROOT}/usr/include -I${BR2_SYSROOT}/usr/include/drm"
export CGO_LDFLAGS="--sysroot=${BR2_SYSROOT} -L${BR2_SYSROOT}/usr/lib -L${BR2_SYSROOT}/lib"
export PKG_CONFIG_PATH=${BR2_SYSROOT}/usr/lib/pkgconfig

# === 开始编译 ===
mkdir -p bin/arm64

echo "Building Executable (go_rtsp)..."
go build -o bin/arm64/go_rtsp cmd/main/main.go

echo "Building Shared Library (libgortsp.so)..."
go build -buildmode=c-shared -ldflags '-extldflags "-Wl,-soname,libgortsp.so"' -o bin/arm64/libgortsp.so cmd/export/export.go

echo "Done."