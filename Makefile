# jvm 构建配置
# 用法: make <target>   (需要 GNU Make, Git Bash / MinGW / WSL 自带)

# ---- 变量 ----
BINARY   := jvm
VERSION  := 0.1.0
DIST     := dist
GO       := go
LDFLAGS  := -s -w
TARGET   := $(DIST)/$(BINARY).exe

# ---- 默认目标 ----
.PHONY: all
all: build

# ---- 构建 ----
# -trimpath 去掉本机路径, -ldflags "-s -w" 去掉调试符号缩小体积
.PHONY: build
build:
	@echo "[build] $(TARGET) (v$(VERSION))..."
	@mkdir -p $(DIST)
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(TARGET) .
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
# 产物: dist/jvm-windows-amd64-setup.exe
.PHONY: installer
installer: build
	@echo "[installer] building setup exe (v$(VERSION))..."
	makensis /DAPP_VERSION=$(VERSION) installer/jvm.nsi
	@echo "[ok]   $(DIST)/$(BINARY)-windows-amd64-setup.exe"

# ---- 发 release 时用: 生成便携 zip ----
# 产物 dist/jvm-windows-amd64.zip (内含单个 jvm.exe, 供 jvm upgrade 精确匹配拉取)
# 用 PowerShell Compress-Archive, 不依赖 zip 命令 (Windows / CI 都自带 PowerShell)
.PHONY: release
release: build
	@powershell -NoProfile -Command "Compress-Archive -Path $(DIST)\$(BINARY).exe -DestinationPath $(DIST)\$(BINARY)-windows-amd64.zip -Force"
	@echo "[release] $(DIST)/$(BINARY)-windows-amd64.zip"
	@echo "          上传到 GitHub Release (tag: v$(VERSION)) 后即可用 jvm upgrade 自更新"

# ---- 一键产出全部发行资产 (安装器 + 便携 zip) ----
.PHONY: dist-all
dist-all: installer release
	@echo "[dist-all] both assets ready under $(DIST)/"

# ---- 帮助 ----
.PHONY: help
help:
	@echo "jvm build targets:"
	@echo "  make build                    build to $(DIST)/$(BINARY).exe"
	@echo "  make run ARGS=\"version\"        build and run"
	@echo "  make installer                build NSIS setup -> $(DIST)/$(BINARY)-windows-amd64-setup.exe"
	@echo "  make release                  build portable zip -> $(DIST)/$(BINARY)-windows-amd64.zip"
	@echo "  make dist-all                 build both release assets"
	@echo "  make clean                    remove $(DIST)/"
	@echo "  make tidy                     go mod tidy"
	@echo "  make fmt                      format code"
	@echo "  make vet                      static check"
