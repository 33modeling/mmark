(function () {
  "use strict";

  var config = window.__MMARK__ || {};
  var article = document.querySelector(".markdown-body");
  if (!article) return;

  var theme = config.theme || "auto";
  var darkQuery = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;
  var mermaidRun = 0;

  function setupPolling() {
    var stamp = config.stamp || "";
    var statusURL = "/__mmark/status?p=" + encodeURIComponent(config.path || "/");
    setInterval(function () {
      fetch(statusURL).then(function (r) { return r.json(); }).then(function (j) {
        if (j.stamp !== stamp) location.reload();
      }).catch(function () {});
    }, 1000);
  }

  function resolvedTheme() {
    if (theme === "dark" || theme === "light") return theme;
    return darkQuery && darkQuery.matches ? "dark" : "light";
  }

  function applyTheme(t) {
    var light = document.getElementById("css-light");
    var dark = document.getElementById("css-dark");
    var btn = document.getElementById("theme-toggle");
    if (t === "light") {
      light.media = "all";
      dark.media = "not all";
    } else if (t === "dark") {
      light.media = "not all";
      dark.media = "all";
    } else {
      light.media = "(prefers-color-scheme: light)";
      dark.media = "(prefers-color-scheme: dark)";
    }
    var icons = { auto: "🌗", light: "☀️", dark: "🌙" };
    var labels = { auto: "자동", light: "라이트", dark: "다크" };
    if (btn) {
      btn.textContent = icons[t] || icons.auto;
      btn.title = "테마: " + (labels[t] || labels.auto);
    }
    document.documentElement.dataset.mmarkTheme = resolvedTheme();
  }

  function setupTheme() {
    var themes = ["auto", "light", "dark"];
    var btn = document.getElementById("theme-toggle");
    if (btn) {
      btn.addEventListener("click", function () {
        theme = themes[(themes.indexOf(theme) + 1) % themes.length];
        applyTheme(theme);
        fetch("/__mmark/theme?set=" + theme, { method: "POST" }).catch(function () {});
        renderMermaidDiagrams();
      });
    }
    if (darkQuery) {
      var onChange = function () {
        if (theme === "auto") {
          applyTheme(theme);
          renderMermaidDiagrams();
        }
      };
      if (darkQuery.addEventListener) darkQuery.addEventListener("change", onChange);
      else if (darkQuery.addListener) darkQuery.addListener(onChange);
    }
    applyTheme(theme);
  }

  function setupPrint() {
    var btn = document.getElementById("print-doc");
    if (btn) btn.addEventListener("click", function () { window.print(); });
  }

  function setupOpenFile() {
    var btn = document.getElementById("open-file");
    if (btn) btn.addEventListener("click", function () { location.href = "/__mmark/pick"; });
  }

  function setupToc() {
    var toc = document.getElementById("toc");
    var btn = document.getElementById("toc-toggle");
    if (!toc || !btn) return;

    var headings = Array.prototype.slice.call(article.querySelectorAll("h1, h2, h3, h4")).filter(function (h) {
      return h.id && h.textContent.trim();
    });
    if (!headings.length) return;

    var list = document.createElement("ol");
    var links = [];
    headings.forEach(function (heading) {
      var li = document.createElement("li");
      var a = document.createElement("a");
      li.className = "toc-level-" + heading.tagName.slice(1).toLowerCase();
      a.href = "#" + encodeURIComponent(heading.id);
      a.textContent = heading.textContent.trim();
      li.appendChild(a);
      list.appendChild(li);
      links.push({ heading: heading, link: a });
    });
    toc.appendChild(list);
    toc.hidden = false;
    btn.hidden = false;
    document.body.classList.add("has-toc");

    btn.addEventListener("click", function () {
      if (window.matchMedia && window.matchMedia("(max-width: 1260px)").matches) {
        document.body.classList.toggle("toc-open");
      } else {
        document.body.classList.toggle("toc-collapsed");
      }
    });

    var ticking = false;
    function updateActive() {
      ticking = false;
      var active = links[0];
      for (var i = 0; i < links.length; i++) {
        if (links[i].heading.getBoundingClientRect().top <= 90) active = links[i];
        else break;
      }
      links.forEach(function (item) { item.link.classList.toggle("is-active", item === active); });
    }
    window.addEventListener("scroll", function () {
      if (!ticking) {
        ticking = true;
        requestAnimationFrame(updateActive);
      }
    }, { passive: true });
    updateActive();
  }

  function copyText(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      return navigator.clipboard.writeText(text);
    }
    return new Promise(function (resolve, reject) {
      var ta = document.createElement("textarea");
      ta.value = text;
      ta.style.position = "fixed";
      ta.style.left = "-9999px";
      document.body.appendChild(ta);
      ta.focus();
      ta.select();
      try {
        document.execCommand("copy") ? resolve() : reject(new Error("copy failed"));
      } catch (err) {
        reject(err);
      } finally {
        ta.remove();
      }
    });
  }

  function setupCodeCopy() {
    Array.prototype.forEach.call(article.querySelectorAll("pre"), function (pre) {
      if (pre.closest(".mmark-mermaid") || pre.parentElement.classList.contains("mmark-code")) return;
      var wrapper = document.createElement("div");
      var btn = document.createElement("button");
      wrapper.className = "mmark-code";
      btn.className = "mmark-copy";
      btn.type = "button";
      btn.textContent = "⧉";
      btn.title = "코드 복사";
      pre.parentNode.insertBefore(wrapper, pre);
      wrapper.appendChild(pre);
      wrapper.appendChild(btn);
      btn.addEventListener("click", function () {
        copyText(pre.textContent).then(function () {
          btn.textContent = "✓";
          window.setTimeout(function () { btn.textContent = "⧉"; }, 900);
        }).catch(function () {
          btn.textContent = "!";
          window.setTimeout(function () { btn.textContent = "⧉"; }, 900);
        });
      });
    });
  }

  function isEscaped(text, index) {
    var count = 0;
    for (var i = index - 1; i >= 0 && text.charAt(i) === "\\"; i--) count++;
    return count % 2 === 1;
  }

  function isSingleDollar(text, index) {
    return text.charAt(index) === "$" &&
      text.charAt(index - 1) !== "$" &&
      text.charAt(index + 1) !== "$" &&
      !isEscaped(text, index);
  }

  function looksLikeInlineMath(source) {
    var text = source.trim();
    if (!text || /^\d+(?:[.,]\d+)?$/.test(text)) return false;
    if (/[\\_^{}=+\-*/<>]|[∑∫√∞≈≠≤≥]/.test(text)) return true;
    if (/^[A-Za-z](?:\d+)?$/.test(text)) return true;
    return /[A-Za-z]\s*[=+\-*/^_]|[=+\-*/^_]\s*[A-Za-z0-9]/.test(text);
  }

  function skipMathTextNode(node) {
    var el = node.parentElement;
    while (el && el !== article) {
      if (/^(SCRIPT|NOSCRIPT|STYLE|TEXTAREA|PRE|CODE|OPTION)$/i.test(el.tagName)) return true;
      if (el.classList.contains("katex") || el.classList.contains("mmark-math") || el.classList.contains("mmark-mermaid")) return true;
      el = el.parentElement;
    }
    return false;
  }

  function protectInlineDollarMath(root) {
    var walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
      acceptNode: function (node) {
        if (!node.nodeValue || node.nodeValue.indexOf("$") < 0 || skipMathTextNode(node)) {
          return NodeFilter.FILTER_REJECT;
        }
        return NodeFilter.FILTER_ACCEPT;
      }
    });
    var nodes = [];
    while (walker.nextNode()) nodes.push(walker.currentNode);
    nodes.forEach(function (node) {
      var text = node.nodeValue;
      var frag = document.createDocumentFragment();
      var pos = 0;
      var changed = false;
      for (var i = 0; i < text.length; i++) {
        if (!isSingleDollar(text, i)) continue;
        var end = -1;
        for (var j = i + 1; j < text.length; j++) {
          if (text.charAt(j) === "\n") break;
          if (isSingleDollar(text, j)) {
            end = j;
            break;
          }
        }
        if (end < 0) continue;
        var source = text.slice(i + 1, end);
        if (!looksLikeInlineMath(source)) {
          i = end;
          continue;
        }
        if (i > pos) frag.appendChild(document.createTextNode(text.slice(pos, i)));
        frag.appendChild(document.createTextNode("\\(" + source + "\\)"));
        pos = end + 1;
        i = end;
        changed = true;
      }
      if (!changed) return;
      if (pos < text.length) frag.appendChild(document.createTextNode(text.slice(pos)));
      node.parentNode.replaceChild(frag, node);
    });
  }

  function renderProtectedMath() {
    if (!window.katex) return;
    Array.prototype.forEach.call(article.querySelectorAll(".mmark-math"), function (node) {
      if (node.dataset.rendered === "true") return;
      var source = node.textContent;
      var display = node.dataset.display === "true";
      try {
        window.katex.render(source, node, {
          displayMode: display,
          throwOnError: false
        });
        node.dataset.rendered = "true";
      } catch (err) {
        node.classList.add("is-error");
        node.title = (err && err.message) ? err.message : String(err);
      }
    });
  }

  function setupMath() {
    renderProtectedMath();
    if (!window.renderMathInElement) return;
    protectInlineDollarMath(article);
    window.renderMathInElement(article, {
      delimiters: [
        { left: "$$", right: "$$", display: true },
        { left: "\\[", right: "\\]", display: true },
        { left: "\\(", right: "\\)", display: false }
      ],
      throwOnError: false,
      ignoredTags: ["script", "noscript", "style", "textarea", "pre", "code", "option"],
      ignoredClasses: ["katex", "mmark-math", "mmark-mermaid"]
    });
  }

  function prepareMermaid() {
    if (!window.mermaid) return;
    var blocks = article.querySelectorAll("pre > code.language-mermaid, pre > code.lang-mermaid, pre > code[data-lang='mermaid']");
    Array.prototype.forEach.call(blocks, function (code) {
      var host = document.createElement("div");
      host.className = "mmark-mermaid";
      host.dataset.source = code.textContent;
      code.parentElement.replaceWith(host);
    });
    renderMermaidDiagrams();
  }

  function renderMermaidDiagrams() {
    if (!window.mermaid) return;
    var blocks = Array.prototype.slice.call(article.querySelectorAll(".mmark-mermaid"));
    if (!blocks.length) return;
    var run = ++mermaidRun;
    window.mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      theme: resolvedTheme() === "dark" ? "dark" : "default"
    });
    blocks.forEach(function (block, index) {
      var source = block.dataset.source || block.textContent;
      var id = "mmark-mermaid-" + run + "-" + index;
      block.classList.remove("is-error");
      block.classList.add("is-rendering");
      Promise.resolve(window.mermaid.render(id, source)).then(function (result) {
        if (run !== mermaidRun) return;
        block.innerHTML = result.svg;
        block.classList.remove("is-rendering");
        if (result.bindFunctions) result.bindFunctions(block);
      }).catch(function (err) {
        if (run !== mermaidRun) return;
        var pre = document.createElement("pre");
        var code = document.createElement("code");
        code.textContent = (err && err.message) ? err.message : String(err);
        pre.appendChild(code);
        block.innerHTML = "";
        block.appendChild(pre);
        block.classList.remove("is-rendering");
        block.classList.add("is-error");
      });
    });
  }

  function setupSearch() {
    var panel = document.getElementById("search-panel");
    var input = document.getElementById("search-input");
    var counter = document.getElementById("search-count");
    var openBtn = document.getElementById("search-open");
    var prevBtn = document.getElementById("search-prev");
    var nextBtn = document.getElementById("search-next");
    var closeBtn = document.getElementById("search-close");
    if (!panel || !input || !counter || !prevBtn || !nextBtn || !closeBtn) return;

    var marks = [];
    var active = -1;
    var searchTimer = 0;

    function cancelScheduledSearch() {
      if (!searchTimer) return;
      window.clearTimeout(searchTimer);
      searchTimer = 0;
    }

    function clearMarks() {
      marks.forEach(function (mark) {
        var parent = mark.parentNode;
        if (!parent) return;
        parent.replaceChild(document.createTextNode(mark.textContent), mark);
        parent.normalize();
      });
      marks = [];
      active = -1;
    }

    function skipTextNode(node) {
      var el = node.parentElement;
      while (el && el !== article) {
        if (/^(SCRIPT|STYLE|TEXTAREA|INPUT|BUTTON)$/i.test(el.tagName)) return true;
        if (el.classList.contains("katex") || el.classList.contains("mmark-mermaid")) return true;
        el = el.parentElement;
      }
      return false;
    }

    function highlightTextNode(node, query, lowerQuery) {
      var text = node.nodeValue;
      var lower = text.toLowerCase();
      var index = lower.indexOf(lowerQuery);
      if (index < 0) return;
      var frag = document.createDocumentFragment();
      var pos = 0;
      while (index >= 0) {
        if (index > pos) frag.appendChild(document.createTextNode(text.slice(pos, index)));
        var mark = document.createElement("mark");
        mark.className = "mmark-search-hit";
        mark.textContent = text.slice(index, index + query.length);
        frag.appendChild(mark);
        marks.push(mark);
        pos = index + query.length;
        index = lower.indexOf(lowerQuery, pos);
      }
      if (pos < text.length) frag.appendChild(document.createTextNode(text.slice(pos)));
      node.parentNode.replaceChild(frag, node);
    }

    function runSearch() {
      cancelScheduledSearch();
      clearMarks();
      var query = input.value;
      var lowerQuery = query.toLowerCase();
      if (!query) {
        counter.textContent = "0/0";
        return;
      }
      var walker = document.createTreeWalker(article, NodeFilter.SHOW_TEXT, {
        acceptNode: function (node) {
          if (!node.nodeValue || skipTextNode(node)) return NodeFilter.FILTER_REJECT;
          return node.nodeValue.toLowerCase().indexOf(lowerQuery) >= 0 ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_REJECT;
        }
      });
      var nodes = [];
      while (walker.nextNode()) nodes.push(walker.currentNode);
      nodes.forEach(function (node) { highlightTextNode(node, query, lowerQuery); });
      if (marks.length) activate(0);
      else counter.textContent = "0/0";
    }

    function scheduleSearch() {
      cancelScheduledSearch();
      if (!input.value) {
        runSearch();
        return;
      }
      counter.textContent = "...";
      searchTimer = window.setTimeout(function () {
        searchTimer = 0;
        runSearch();
      }, 120);
    }

    function flushSearch() {
      if (searchTimer) runSearch();
    }

    function activate(index) {
      if (!marks.length) {
        active = -1;
        counter.textContent = "0/0";
        return;
      }
      if (active >= 0 && marks[active]) marks[active].classList.remove("is-active");
      active = (index + marks.length) % marks.length;
      marks[active].classList.add("is-active");
      counter.textContent = (active + 1) + "/" + marks.length;
      marks[active].scrollIntoView({ block: "center", inline: "nearest" });
    }

    function openSearch() {
      panel.hidden = false;
      input.focus();
      input.select();
      runSearch();
    }

    function closeSearch() {
      panel.hidden = true;
      cancelScheduledSearch();
      clearMarks();
      input.value = "";
      counter.textContent = "0/0";
    }

    input.addEventListener("input", scheduleSearch);
    prevBtn.addEventListener("click", function () {
      flushSearch();
      activate(active - 1);
    });
    nextBtn.addEventListener("click", function () {
      flushSearch();
      activate(active + 1);
    });
    closeBtn.addEventListener("click", closeSearch);
    if (openBtn) openBtn.addEventListener("click", openSearch);

    document.addEventListener("keydown", function (e) {
      var tag = e.target && e.target.tagName ? e.target.tagName.toUpperCase() : "";
      var typing = tag === "INPUT" || tag === "TEXTAREA" || e.target.isContentEditable;
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "f") {
        e.preventDefault();
        openSearch();
        return;
      }
      if (!typing && e.key === "/") {
        e.preventDefault();
        openSearch();
        return;
      }
      if (!panel.hidden && e.key === "Escape") {
        e.preventDefault();
        closeSearch();
        return;
      }
      if (!panel.hidden && e.target === input && e.key === "Enter") {
        e.preventDefault();
        flushSearch();
        activate(active + (e.shiftKey ? -1 : 1));
      }
    });
  }

  setupTheme();
  setupOpenFile();
  setupPrint();
  setupMath();
  prepareMermaid();
  setupCodeCopy();
  setupToc();
  setupSearch();
  setupPolling();
})();
