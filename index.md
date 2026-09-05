---
layout: landing
title: 首页
---

<!-- ================= Hero（左文右终端） ================= -->
<section class="hero">
  <div class="container hero-split">
    <div class="hero-text">
      <span class="hero-badge">开源 · MIT · Windows x64 / ARM64 原生</span>
      <h1>Windows 上的<br><span class="grad">Java 版本管理器</span></h1>
      <p class="hero-sub">
        类似 nvm-windows / jabba 的 JDK 版本管理工具。
        一键安装、秒级切换、自动配置 PATH 与 JAVA_HOME，全程免管理员权限。
      </p>

      <div class="install-cmd">
        <code id="hero-install-cmd">iwr -useb "https://raw.githubusercontent.com/BaixuanZhu/jvm/main/install.ps1" | iex</code>
        <button class="copy-btn" data-copy="#hero-install-cmd">复制</button>
      </div>

      <div class="hero-actions">
        <a class="btn btn-primary" href="{{ site.download_setup }}">下载安装包</a>
        <a class="btn btn-ghost" href="{{ site.github_repo }}" target="_blank" rel="noopener">GitHub ★</a>
      </div>
      <p class="hero-note">安装脚本自动识别 x64 / ARM64；ARM64 设备也可<a href="{{ site.download_setup_arm64 }}">下载 ARM64 安装包</a>。</p>
    </div>

    <!-- 无 JS / 减弱动效时显示静态内容；否则替换为打字动画 -->
    <div class="terminal hero-term">
      <div class="terminal-bar">
        <span class="dot r"></span><span class="dot y"></span><span class="dot g"></span>
        <span class="term-title">PowerShell</span>
      </div>
      <pre id="demo-term"><span class="p">jvm install 21</span>

<span class="p">jvm use 21</span>

<span class="p">cd my-app</span>  <span class="c"># 项目里有 .jvmrc: corretto@17</span>

<span class="o">java -version</span>  <span class="c"># 自动切到 17, 现在就是它</span></pre>
    </div>
  </div>
</section>

<!-- ================= 特性 ================= -->
<section class="section section-alt">
  <div class="container">
    <h2 class="section-title fade-in">为 Windows Java 开发者而生</h2>
    <p class="section-sub fade-in">从下载到切换，每一步都替你做好。</p>

    <div class="feature-grid">
      <div class="feature-card fade-in">
        <div class="icon">📦</div>
        <h3>一键安装</h3>
        <p><code>jvm install 21</code> 自动下载最新 GA 版本，官方哈希校验（SHA256 / SHA1），损坏即报错中止。</p>
      </div>
      <div class="feature-card fade-in">
        <div class="icon">🔄</div>
        <h3>秒级切换</h3>
        <p><code>jvm use 21</code> 基于 Windows junction 切换，新终端自动生效，无需刷新环境变量。</p>
      </div>
      <div class="feature-card fade-in">
        <div class="icon">⚡</div>
        <h3>当前终端即时生效</h3>
        <p>自动注入 shell 函数，<code>use</code> 之后当前窗口的 <code>java</code> 立刻变更，不用重开。</p>
      </div>
      <div class="feature-card fade-in">
        <div class="icon">🌐</div>
        <h3>多发行版</h3>
        <p>Temurin / Corretto / Microsoft / Zulu / Liberica / GraalVM，<code>distro@version</code> 语法随心选。</p>
      </div>
      <div class="feature-card fade-in">
        <div class="icon">⬆️</div>
        <h3>Patch 一键升级</h3>
        <p><code>jvm update 21</code> 装最新 patch、自动切换、清理旧版一条龙；<code>update --all</code> 批量升完全部落后组。</p>
      </div>
      <div class="feature-card fade-in">
        <div class="icon">💾</div>
        <h3>缓存与离线</h3>
        <p>安装包缓存重装免重下，<code>available</code> 结果 10 分钟缓存提速；内网环境还能装本地 zip 包。</p>
      </div>
      <div class="feature-card fade-in">
        <div class="icon">📄</div>
        <h3>项目级 .jvmrc</h3>
        <p><code>jvm pin</code> 把版本固定在项目里，cd 进目录自动切换、cd 出去自动恢复，团队统一 JDK。</p>
      </div>
      <div class="feature-card fade-in">
        <div class="icon">🧪</div>
        <h3>多版本并行</h3>
        <p><code>jvm exec 17 -- mvn test</code> 用指定版本一次性执行，不动全局，多版本对比测试神器。</p>
      </div>
      <div class="feature-card fade-in">
        <div class="icon">🚫</div>
        <h3>免管理员权限</h3>
        <p>junction 走原生 syscall，注册表只写 HKCU，全程无需提权。</p>
      </div>
      <div class="feature-card fade-in">
        <div class="icon">🏠</div>
        <h3>国内加速</h3>
        <p>Temurin 优先走清华镜像，失败自动回退官方 CDN；下载支持断点续传。</p>
      </div>
    </div>
  </div>
</section>

<!-- ================= 发行版 ================= -->
<section class="section section-alt">
  <div class="container">
    <h2 class="section-title fade-in">七大发行版，一个命令</h2>
    <p class="section-sub fade-in">省略前缀默认 Temurin，精确到小版本也没问题。</p>

    <div class="distro-grid">
      <div class="distro-card fade-in">
        <h3>Temurin</h3>
        <p class="vendor">Eclipse Adoptium · 默认 · 清华镜像加速</p>
        <code>jvm install 21</code>
      </div>
      <div class="distro-card fade-in">
        <h3>Corretto</h3>
        <p class="vendor">Amazon · 含 SHA256 校验清单</p>
        <code>jvm install corretto@21</code>
      </div>
      <div class="distro-card fade-in">
        <h3>Microsoft</h3>
        <p class="vendor">Microsoft Build of OpenJDK · 仅 LTS</p>
        <code>jvm install microsoft@21</code>
      </div>
      <div class="distro-card fade-in">
        <h3>Zulu</h3>
        <p class="vendor">Azul · 覆盖最全的 LTS / 最新版</p>
        <code>jvm install zulu@21</code>
      </div>
      <div class="distro-card fade-in">
        <h3>Liberica</h3>
        <p class="vendor">BellSoft · 全架构 · 仅 LTS</p>
        <code>jvm install liberica@21</code>
      </div>
      <div class="distro-card fade-in">
        <h3>GraalVM</h3>
        <p class="vendor">Oracle · CPU LTS 线（21/25）· 官方 CDN</p>
        <code>jvm install graalvm@21</code>
      </div>
      <div class="distro-card fade-in">
        <h3>Temurin EA</h3>
        <p class="vendor">Adoptium 早期访问版 · GA 前预览</p>
        <code>jvm install temurin-ea@28</code>
      </div>
    </div>

    <p class="distro-note fade-in">精确版本同样支持：<code>jvm install 21.0.12+8</code>；内网环境可装本地 zip：<code>jvm install temurin@21.0.5+11 D:\jdk.zip</code><br>
    ARM64 设备自动下载 ARM64 版 JDK（Temurin / Microsoft / Zulu / Liberica；Corretto 与 GraalVM 官方暂无 Windows ARM64 构建）。</p>
  </div>
</section>

<!-- ================= CTA ================= -->
<section class="section cta">
  <div class="container">
    <h2 class="section-title fade-in">现在就试试</h2>
    <p class="section-sub fade-in">一行命令，一分钟装好，随时卸载。</p>
    <div class="hero-actions fade-in">
      <a class="btn btn-primary" href="{{ '/docs/install/' | relative_url }}">查看安装指南</a>
      <a class="btn btn-ghost" href="{{ site.github_repo }}" target="_blank" rel="noopener">在 GitHub 上 Star</a>
    </div>
  </div>
</section>
