# jvm 构建配置
# 用法: make <target>   (需要 GNU Make, Git Bash / MinGW / WSL 自带)

# ---- 变量 ----
BINARY   := jvm
# 版本号: 优先用 git describe (tag 或 commit); 否则回退默认值。
# 构建时通过 ldflags 注入 internal/app.Version, 源码里不再手写版本字面量。
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
DEFAULT_VERSION := 0.1.0-dev
ifeq ($(VERSION),)
VERSION := $(DEFAULT_VERSION)
endif
DIST     := dist
GO       := go
# 目标架构: 默认取本机 (go env GOARCH); 交叉编 ARM64 用 make build GOARCH=arm64。
# 产物按架构分目录存放 (dist/<arch>/jvm.exe), 避免双架构构建互相覆盖。
GOARCH   ?= $(shell $(GO) env GOARCH)
LDFLAGS  := -s -w -X jvm/internal/app.Version=$(VERSION)
# 开发构建额外注入 Bootstrap=off: dist/ 下的开发版启动时不做静默自举
# (不写用户 PATH / shell profile), 避免污染环境; 发行构建 (build-dist) 不带此项。
LDFLAGS_DEV := $(LDFLAGS) -X jvm/internal/app.Bootstrap=off
TARGET   := $(DIST)/$(GOARCH)/$(BINARY).exe

# ---- 默认目标 ----
.PHONY: all
all: build

# ---- 构建 (开发用: 关闭启动自举, 不污染 PATH/profile) ----
# -trimpath 去掉本机路径, -ldflags "-s -w" 去掉调试符号缩小体积
# GOOS 固定 windows (项目仅支持 Windows), GOARCH 可传 arm64 交叉编译
.PHONY: build
build:
	@echo "[build] $(TARGET) (v$(VERSION), windows/$(GOARCH), dev: 不自举)..."
	@mkdir -p $(DIST)/$(GOARCH)
	GOOS=windows GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags "$(LDFLAGS_DEV)" -o $(TARGET) .
	@echo "[ok]   $(TARGET)"
	@ls -lh $(TARGET) | awk '{print "       size:", $$5}'

# ---- 构建 (发行风味: 保持启动自举, 供 installer/release 打包与 CI 使用) ----
.PHONY: build-dist
build-dist:
	@echo "[build-dist] $(TARGET) (v$(VERSION), windows/$(GOARCH))..."
	@mkdir -p $(DIST)/$(GOARCH)
	GOOS=windows GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(TARGET) .
	@echo "[ok]   $(TARGET)"
	@ls -lh $(TARGET) | awk '{print "       size:", $$5}'

# ---- 构建并运行 (传参: make run ARGS="version") ----
.PHONY: run
run: build
	./$(TARGET) $(ARGS)

# ---- 清理 ----
.PHONY: clean
clean:
	@rm -rf $(DIST)
	@echo "[clean] removed $(DIST)/"

# ---- 依赖管理 ----
.PHONY: tidy
tidy:
	$(GO) mod tidy

# ---- 代码检查 ----
.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: vet
vet:
	$(GO) vet ./...

# ---- NSIS 安装包 (需 makensis 在 PATH; scoop install nsis 或 choco install nsis) ----
# 产物: dist/jvm-windows-$(GOARCH)-setup.exe (ARM64 用 GOARCH=arm64)
# 安装器 stub 是 x86 的, 在 ARM64 Windows 上靠系统内置模拟运行,
# 释放出来的 jvm.exe 仍是目标架构原生二进制。
.PHONY: installer
installer: build-dist
	@echo "[installer] building setup exe (v$(VERSION), $(GOARCH))..."
	makensis /DAPP_VERSION=$(VERSION) /DAPP_ARCH=$(GOARCH) installer/jvm.nsi
	@echo "[ok]   $(DIST)/$(BINARY)-windows-$(GOARCH)-setup.exe"

# ---- 发 release 时用: 生成便携 zip ----
# 产物 dist/jvm-windows-$(GOARCH).zip (内含单个 jvm.exe, 供 jvm upgrade 精确匹配拉取)
# 用 PowerShell Compress-Archive, 不依赖 zip 命令 (Windows / CI 都自带 PowerShell)
.PHONY: release
release: build-dist
	@powershell -NoProfile -Command "Compress-Archive -Path $(DIST)\$(GOARCH)\$(BINARY).exe -DestinationPath $(DIST)\$(BINARY)-windows-$(GOARCH).zip -Force"
	@echo "[release] $(DIST)/$(BINARY)-windows-$(GOARCH).zip"
	@echo "          上传到 GitHub Release (tag: v$(VERSION)) 后即可用 jvm upgrade 自更新"

# ---- 一键产出全部发行资产 (安装器 + 便携 zip, 仅当前 GOARCH) ----
# 双架构发行资产由 CI 分别用 GOARCH=amd64 / GOARCH=arm64 各跑一遍产出。
.PHONY: dist-all
dist-all: installer release
	@echo "[dist-all] both assets ready under $(DIST)/ (windows/$(GOARCH))"

# ---- 帮助 ----
.PHONY: help
help:
	@echo "jvm build targets:"
	@echo "  make build                    dev build (no bootstrap) -> $(DIST)/<arch>/$(BINARY).exe"
	@echo "  make build-dist               release-flavor build (bootstrap on)"
	@echo "  make run ARGS=\"version\"        dev build and run (GOARCH=arm64 交叉编 ARM64)"
	@echo "  make installer                build NSIS setup -> $(DIST)/$(BINARY)-windows-<arch>-setup.exe"
	@echo "  make release                  build portable zip -> $(DIST)/$(BINARY)-windows-<arch>.zip"
	@echo "  make dist-all                 build both release assets (current GOARCH)"
	@echo "  make clean                    remove $(DIST)/"
	@echo "  make tidy                     go mod tidy"
	@echo "  make fmt                      format code"
	@echo "  make vet                      static check"
