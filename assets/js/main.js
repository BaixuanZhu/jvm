// jvm 官网交互：复制按钮、移动端导航、滚动入场动画
(function () {
  "use strict";

  // ---- 复制按钮 ----
  function copyText(text, btn) {
    var done = function () {
      btn.textContent = "已复制";
      btn.classList.add("copied");
      setTimeout(function () {
        btn.textContent = "复制";
        btn.classList.remove("copied");
      }, 1600);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, function () { fallbackCopy(text, done); });
    } else {
      fallbackCopy(text, done);
    }
  }

  function fallbackCopy(text, done) {
    var ta = document.createElement("textarea");
    ta.value = text;
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand("copy"); done(); } catch (e) { /* 忽略 */ }
    document.body.removeChild(ta);
  }

  // Hero 区安装命令
  document.querySelectorAll("[data-copy]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var target = document.querySelector(btn.getAttribute("data-copy"));
      if (target) copyText(target.textContent.trim(), btn);
    });
  });

  // 文档页代码块：包装成「顶栏（语言标签 + 复制按钮）+ 代码区」结构
  document.querySelectorAll(".docs-content pre").forEach(function (pre) {
    // 从 kramdown/rouge 的 language-* 包装类里取语言名
    var lang = "";
    var wrapper = pre.closest("[class*='language-']");
    if (wrapper) {
      var m = wrapper.className.match(/language-([a-zA-Z0-9+#-]+)/);
      if (m) lang = m[1];
    }
    // 无语言的纯文本块统一显示 TEXT
    if (!lang || lang === "plaintext" || lang === "text") lang = "text";

    var block = document.createElement("div");
    block.className = "code-block";
    pre.parentNode.insertBefore(block, pre);

    var header = document.createElement("div");
    header.className = "code-header";

    var label = document.createElement("span");
    label.className = "code-lang";
    label.textContent = lang || "code";

    var btn = document.createElement("button");
    btn.className = "copy-btn";
    btn.textContent = "复制";
    btn.addEventListener("click", function () {
      var code = pre.querySelector("code");
      copyText((code || pre).textContent, btn);
    });

    header.appendChild(label);
    header.appendChild(btn);
    block.appendChild(header);
    block.appendChild(pre);
  });

  // ---- 移动端导航 ----
  var toggle = document.querySelector(".nav-toggle");
  var nav = document.querySelector(".site-nav");
  if (toggle && nav) {
    toggle.addEventListener("click", function () {
      var open = nav.classList.toggle("open");
      toggle.setAttribute("aria-expanded", open ? "true" : "false");
    });
  }

  // ---- 滚动入场动画 ----
  var items = document.querySelectorAll(".fade-in");
  if ("IntersectionObserver" in window && items.length) {
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) {
          entry.target.classList.add("visible");
          io.unobserve(entry.target);
        }
      });
    }, { threshold: 0.12 });
    items.forEach(function (el) { io.observe(el); });
  } else {
    items.forEach(function (el) { el.classList.add("visible"); });
  }

  // ---- 首页演示终端：纯前端打字动画（无 JS / 减弱动效时保留静态内容） ----
  var demo = document.getElementById("demo-term");
  var reduceMotion = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (demo && !reduceMotion) {
    // [样式类, 文本]，cmd 为打字输入的命令，out 为逐行浮现的输出
    var script = [
      { type: "cmd", text: "jvm install 21" },
      { type: "out", lines: [
        ["dim", "正在解析 temurin@21 ..."],
        ["dim", "下载 OpenJDK 21.0.12+8  [████████████] 100%"],
        ["o",   "✓ SHA256 校验通过"],
        ["o",   "✓ 安装完成：temurin@21.0.12+8"]
      ]},
      { type: "cmd", text: "jvm use 21" },
      { type: "out", lines: [
        ["o",   "✓ 已切换到 temurin@21.0.12+8"],
        ["dim", "当前终端同步生效，无需重开"]
      ]},
      { type: "cmd", text: "java -version" },
      { type: "out", lines: [
        ["o",   'openjdk version "21.0.12" 2026-07-21 LTS'],
        ["dim", "OpenJDK Runtime Environment Temurin-21.0.12+8"]
      ]}
    ];

    var step = 0;

    function newline() { demo.appendChild(document.createTextNode("\n")); }

    function typeCommand(text, done) {
      if (demo.textContent.length > 0) newline(); // 命令块之间空一行
      var prompt = document.createElement("span");
      prompt.className = "dim";
      prompt.textContent = "$ ";
      var typed = document.createElement("span");
      typed.className = "p";
      var cursor = document.createElement("span");
      cursor.className = "cursor";
      demo.appendChild(prompt);
      demo.appendChild(typed);
      demo.appendChild(cursor);

      var i = 0;
      var timer = setInterval(function () {
        typed.textContent = text.slice(0, ++i);
        if (i >= text.length) {
          clearInterval(timer);
          cursor.remove();
          newline();
          setTimeout(done, 420);
        }
      }, 55);
    }

    function showLines(lines, idx, done) {
      if (idx >= lines.length) { setTimeout(done, 500); return; }
      var span = document.createElement("span");
      span.className = lines[idx][0];
      span.textContent = lines[idx][1];
      demo.appendChild(span);
      newline();
      setTimeout(function () { showLines(lines, idx + 1, done); }, 360);
    }

    function runStep() {
      if (step >= script.length) {
        // 一轮结束，停留片刻后清屏重来
        setTimeout(function () {
          demo.innerHTML = "";
          step = 0;
          runStep();
        }, 5200);
        return;
      }
      var s = script[step++];
      if (s.type === "cmd") {
        typeCommand(s.text, runStep);
      } else {
        showLines(s.lines, 0, runStep);
      }
    }

    demo.innerHTML = "";
    runStep();
  }
})();
