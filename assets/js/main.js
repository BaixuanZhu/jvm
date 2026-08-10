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
})();
