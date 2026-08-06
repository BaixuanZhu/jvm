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

# ---- 交叉编译示例 (发 release 时用) ----
# 生成 dist/jvm-windows-amd64.exe.zip
.PHONY: release
release: build
	@cd $(DIST) && zip -j $(BINARY)-windows-amd64.exe.zip $(BINARY).exe
	@echo "[release] $(DIST)/$(BINARY)-windows-amd64.exe.zip"
	@echo "          上传到 GitHub Release (tag: v$(VERSION)) 后即可用 jvm upgrade 自更新"

# ---- 帮助 ----
.PHONY: help
help:
	@echo "jvm build targets:"
	@echo "  make build                    build to $(DIST)/$(BINARY).exe"
	@echo "  make run ARGS=\"version\"        build and run"
	@echo "  make clean                    remove $(DIST)/"
	@echo "  make tidy                     go mod tidy"
	@echo "  make fmt                      format code"
	@echo "  make vet                      static check"
	@echo "  make release                  build and zip for GitHub Release"
