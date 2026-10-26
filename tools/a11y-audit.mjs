// @ts-check
/**
 * 可访问性走查（NFR-04 / AC-NFR-04 / T-18）。开发工具，不进产物。
 *
 * 量四件事：
 *   1. 正文与关键文字的对比度（WCAG 2.1 AA：小字 ≥ 4.5:1、大字 ≥ 3:1）
 *   2. 可点元素的触摸目标（≥ 48×48px）
 *   3. 语义与可访问名（aria-label / 可见文字 / 图片 alt / 对话框角色）
 *   4. 200% 缩放（等价于把布局视口宽度减半）后是否破版
 *
 * 用法：node tools/a11y-audit.mjs <baseURL> <cookie> [outJSON]
 */

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const base = process.argv[2];
const cookie = process.argv[3];
const outPath = process.argv[4] || "";

const profile = mkdtempSync(join(tmpdir(), "rt-a11y-"));
const PORT = 9347;
const chrome = spawn("C:/Program Files/Google/Chrome/Application/chrome.exe",
  ["--headless=new", `--remote-debugging-port=${PORT}`, `--user-data-dir=${profile}`,
   "--no-first-run", "--disable-gpu", "about:blank"], { stdio: "ignore" });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

let wsUrl = null;
for (let i = 0; i < 80 && !wsUrl; i++) {
  try { wsUrl = (await (await fetch(`http://127.0.0.1:${PORT}/json/version`)).json()).webSocketDebuggerUrl; } catch (e) { /* wait */ }
  if (!wsUrl) await sleep(150);
}
const ws = new WebSocket(wsUrl);
await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej; });

let seq = 0;
const pending = new Map();
/** @type {string[]} */
const jsErrors = [];
ws.onmessage = (ev) => {
  const m = JSON.parse(ev.data);
  if (m.id && pending.has(m.id)) {
    const p = pending.get(m.id); pending.delete(m.id);
    m.error ? p.reject(new Error(m.error.message)) : p.resolve(m.result);
    return;
  }
  if (m.method === "Runtime.exceptionThrown") {
    jsErrors.push(m.params.exceptionDetails?.exception?.description || m.params.exceptionDetails?.text);
  }
};
/** @param {string} method @param {any} params @param {string} [sid] */
function send(method, params = {}, sid) {
  const id = ++seq;
  ws.send(JSON.stringify({ id, method, params, ...(sid ? { sessionId: sid } : {}) }));
  return new Promise((resolve, reject) => pending.set(id, { resolve, reject }));
}

const { targetId } = await send("Target.createTarget", { url: "about:blank" });
const { sessionId } = await send("Target.attachToTarget", { targetId, flatten: true });
const S = (m, p = {}) => send(m, p, sessionId);

await S("Page.enable");
await S("Runtime.enable");
await S("Network.enable");
await S("Network.setCacheDisabled", { cacheDisabled: true });
await S("Network.setCookie", { name: "rt_session", value: cookie, domain: "127.0.0.1", path: "/", httpOnly: true });

const DOC = "/doc/%E4%BA%BA%E5%B7%A5%E6%99%BA%E8%83%BD/%E6%8A%80%E8%83%BD.md";

/** 页面内跑的审计脚本：对比度 + 触摸目标 + 可访问名。 */
const AUDIT_JS = String.raw`(() => {
  const parseColor = (s) => {
    const m = s.match(/rgba?\(([^)]+)\)/);
    if (!m) return null;
    const p = m[1].split(",").map((x) => parseFloat(x.trim()));
    return { r: p[0], g: p[1], b: p[2], a: p.length > 3 ? p[3] : 1 };
  };
  const lum = ({ r, g, b }) => {
    const f = (v) => {
      const c = v / 255;
      return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
    };
    return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
  };
  const blend = (fg, bg) => ({
    r: fg.r * fg.a + bg.r * (1 - fg.a),
    g: fg.g * fg.a + bg.g * (1 - fg.a),
    b: fg.b * fg.a + bg.b * (1 - fg.a),
    a: 1,
  });
  const ratio = (a, b) => {
    const l1 = lum(a), l2 = lum(b);
    const hi = Math.max(l1, l2), lo = Math.min(l1, l2);
    return (hi + 0.05) / (lo + 0.05);
  };
  /** 沿祖先找第一个不透明背景，并把半透明层依次叠加 */
  const bgOf = (el) => {
    const stack = [];
    let node = el;
    while (node) {
      const c = parseColor(getComputedStyle(node).backgroundColor);
      if (c && c.a > 0) {
        stack.push(c);
        if (c.a >= 0.999) break;
      }
      node = node.parentElement;
    }
    let cur = { r: 255, g: 255, b: 255, a: 1 };
    for (let i = stack.length - 1; i >= 0; i--) cur = blend(stack[i], cur);
    return cur;
  };
  const sample = (label, el) => {
    if (!el) return null;
    const cs = getComputedStyle(el);
    const fg = parseColor(cs.color);
    if (!fg) return null;
    const bg = bgOf(el);
    const px = parseFloat(cs.fontSize);
    const bold = (parseInt(cs.fontWeight, 10) || 400) >= 700;
    const large = px >= 24 || (px >= 18.66 && bold);
    const r = ratio(blend(fg, bg), bg);
    return {
      label,
      color: cs.color,
      bg: "rgb(" + Math.round(bg.r) + ", " + Math.round(bg.g) + ", " + Math.round(bg.b) + ")",
      px: Math.round(px * 10) / 10,
      large,
      ratio: Math.round(r * 100) / 100,
      ok: r >= (large ? 3 : 4.5),
    };
  };
  const root = document.querySelector(".markdown") || document.querySelector("#view");
  const samples = [
    sample("正文段落", root && root.querySelector("p")),
    sample("H2 标题", root && root.querySelector("h2")),
    sample("站内链接", root && root.querySelector("a")),
    sample("行内代码", root && root.querySelector("code")),
    sample("表格表头", root && root.querySelector("th")),
    sample("表格单元格", root && root.querySelector("td")),
    sample("引用块", root && root.querySelector("blockquote p")),
    sample("页脚元信息", document.querySelector(".reader-foot")),
    sample("顶栏标题", document.querySelector(".topbar-title")),
    sample("代码块头标签", document.querySelector(".code-lang")),
  ].filter(Boolean);

  // 可点元素：矩形小于 48 的列出来（含可见文字或 aria-label）。
  // 正文里的行内链接按 WCAG 2.5.8 属于「行内文本例外」，不算触摸目标，
  // 所以排除 .markdown 内 display:inline 的 a——把它们混进来只会淹没真正的控件问题。
  const clickable = [...document.querySelectorAll("a[href], button, [role=button], input, select, textarea, summary")]
    .filter((el) => {
      const cs = getComputedStyle(el);
      const r = el.getBoundingClientRect();
      if (cs.display === "none" || cs.visibility === "hidden" || r.width === 0 || r.height === 0) return false;
      const inlineProseLink = el.tagName === "A" && cs.display === "inline" && el.closest(".markdown");
      return !inlineProseLink;
    });
  // 命中区判定（WCAG 2.5.8 的两种合格情形，本项目都会用到）：
  //  1) 控件被 <label> / label[for] 包住且整行 ≥48 —— 点文字等于点控件；
  //  2) 控件用绝对定位的 ::after/::before 向外扩了命中区。
  const hitAreaOk = (el) => {
    const r = el.getBoundingClientRect();
    if (r.width >= 47 && r.height >= 47) return true;
    const label = el.closest("label") || (el.id ? document.querySelector('label[for="' + el.id + '"]') : null);
    if (label) {
      const lr = label.getBoundingClientRect();
      if (lr.width >= 47 && lr.height >= 47) return true;
    }
    for (const pseudo of ["::after", "::before"]) {
      const cs = getComputedStyle(el, pseudo);
      if (cs.content === "none" || cs.position !== "absolute") continue;
      const t = parseFloat(cs.top), b = parseFloat(cs.bottom);
      const l = parseFloat(cs.left), rt = parseFloat(cs.right);
      const h = r.height - (Number.isFinite(t) ? t : 0) - (Number.isFinite(b) ? b : 0);
      const w = r.width - (Number.isFinite(l) ? l : 0) - (Number.isFinite(rt) ? rt : 0);
      if (w >= 47 && h >= 47) return true;
    }
    return false;
  };
  const small = clickable
    .filter((el) => !hitAreaOk(el))
    .map((el) => {
      const r = el.getBoundingClientRect();
      const name = (el.getAttribute("aria-label") || el.textContent || "").trim().slice(0, 18);
      return {
        tag: el.tagName.toLowerCase(),
        cls: (el.className || "").toString().split(" ")[0],
        name,
        w: Math.round(r.width),
        h: Math.round(r.height),
      };
    })
    .filter((x) => x.w < 47 || x.h < 47);

  // 可访问名缺失
  const nameless = clickable
    .filter((el) => {
      const label = (el.getAttribute("aria-label") || el.textContent || "").trim();
      return label === "" && !el.querySelector("img[alt]");
    })
    .map((el) => el.tagName.toLowerCase() + "." + (el.className || "").toString().split(" ")[0]);

  // 图片 alt、对话框语义、viewport
  const imgsNoAlt = [...document.querySelectorAll("img")].filter((i) => !i.hasAttribute("alt")).length;
  const dialog = document.querySelector("[role=dialog]");
  const viewport = document.querySelector("meta[name=viewport]");
  const reduceMotion = [...document.styleSheets].some((sheet) => {
    try {
      return [...sheet.cssRules].some((r) => r.cssText.includes("prefers-reduced-motion"));
    } catch (e) { return false; }
  });

  return {
    samples,
    small,
    nameless,
    imgsNoAlt,
    dialog: dialog
      ? { modal: dialog.getAttribute("aria-modal"), label: dialog.getAttribute("aria-label") }
      : null,
    viewport: viewport && viewport.getAttribute("content"),
    reduceMotion,
    overflowX: document.scrollingElement.scrollWidth - window.innerWidth,
  };
})()`;

/**
 * @param {string} theme
 * @param {string} path
 * @param {number} width
 */
async function auditPage(theme, path, width) {
  await S("Emulation.setEmulatedMedia", {
    features: [{ name: "prefers-color-scheme", value: theme === "light" ? "light" : "dark" }],
  });
  await S("Emulation.setDeviceMetricsOverride", {
    width, height: 1280, deviceScaleFactor: 2, mobile: true,
  });
  await S("Page.navigate", { url: base + "/" });
  await sleep(700);
  await S("Runtime.evaluate", {
    expression: `localStorage.setItem("readtool.theme", ${JSON.stringify(theme)})`,
  });
  await S("Page.navigate", { url: base + path });
  await sleep(1100);
  const res = await S("Runtime.evaluate", { expression: AUDIT_JS, returnByValue: true });
  const data = res.result.value;
  data.theme = theme;
  data.path = path;
  data.width = width;
  data.appliedTheme = await S("Runtime.evaluate", {
    expression: "document.documentElement.dataset.theme",
    returnByValue: true,
  }).then((x) => x.result.value);
  return data;
}

const report = { pages: [], jsErrors };

// 1) 四个主题下的阅读页
for (const theme of ["light", "dark", "paper", "auto"]) {
  report.pages.push(await auditPage(theme, DOC, 744));
}

// 2) 首页 / 搜索页 / 收藏页：触摸目标与可访问名
for (const path of ["/", "/search?q=%E6%B3%9B%E5%9E%8B", "/bookmarks"]) {
  report.pages.push(await auditPage("dark", path, 744));
}

// 3) 面板打开时的对话框语义 + tab 顺序
await S("Page.navigate", { url: base + DOC });
await sleep(1000);
await S("Runtime.evaluate", { expression: `document.querySelector(".type-btn").click()` });
await sleep(400);
const sheet = await S("Runtime.evaluate", { expression: AUDIT_JS, returnByValue: true });
const sheetExtra = await S("Runtime.evaluate", {
  expression: `(() => {
    const s = document.querySelector(".sheet");
    const focusable = [...s.querySelectorAll("button, a[href]")].map((b) => ({
      name: (b.getAttribute("aria-label") || b.textContent || "").trim().slice(0, 12),
      h: Math.round(b.getBoundingClientRect().height),
    }));
    return { focusables: focusable, activeIsInSheet: !!s.contains(document.activeElement), activeText: (document.activeElement.textContent || "").trim().slice(0, 10) };
  })()`,
  returnByValue: true,
});
report.sheet = { ...sheet.result.value, ...sheetExtra.result.value, theme: "dark", width: 744 };

// 4) 200% 缩放：布局视口腰斩到 372px（浏览器 200% 缩放等价于此）
report.zoom200 = await auditPage("dark", DOC, 372);

// 5) 焦点可见性：Tab 走一圈，记录每次的 activeElement 与 outline
await S("Emulation.setDeviceMetricsOverride", { width: 744, height: 1280, deviceScaleFactor: 2, mobile: true });
await S("Page.navigate", { url: base + DOC });
await sleep(1000);
/** @type {any[]} */
const tabOrder = [];
for (let i = 0; i < 12; i++) {
  await S("Input.dispatchKeyEvent", { type: "rawKeyDown", windowsVirtualKeyCode: 9, key: "Tab", code: "Tab" });
  await S("Input.dispatchKeyEvent", { type: "keyUp", windowsVirtualKeyCode: 9, key: "Tab", code: "Tab" });
  await sleep(90);
  const cur = await S("Runtime.evaluate", {
    expression: `(() => {
      const el = document.activeElement;
      if (!el || el === document.body) return null;
      const cs = getComputedStyle(el);
      return {
        tag: el.tagName.toLowerCase(),
        cls: (el.className || "").toString().split(" ")[0],
        name: (el.getAttribute("aria-label") || el.textContent || "").trim().slice(0, 14),
        outline: cs.outlineStyle + " " + cs.outlineWidth,
      };
    })()`,
    returnByValue: true,
  });
  if (cur.result.value) tabOrder.push(cur.result.value);
}
report.tabOrder = tabOrder;

report.jsErrors = jsErrors;
const json = JSON.stringify(report, null, 2);
if (outPath) writeFileSync(outPath, json);
console.log(json);

ws.close();
chrome.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) { /* ignore */ }
process.exit(0);
