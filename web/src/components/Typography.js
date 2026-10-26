// @ts-check
/**
 * 阅读排版设置（FR-15）。
 *
 * 三组设置 + 一个字体切换：
 *   字号 17 / 19 / 22（默认 19；窄窗由 @container 自动降到 17，用户显式选过才覆盖）
 *   行距 1.6 / 1.75 / 1.9（默认 1.75）
 *   主题 跟随系统 / 浅色 / 深色 / 护眼米黄
 *   字体 系统默认 / 宋体（思源宋体可用时生效，否则退到系统衬线体）
 *
 * 只存 localStorage，不跨设备同步（FR-15 规则 5）。
 * 所有改动都是 CSS 变量 + 流式重排，不会出现横向溢出（AC-15-1）。
 */

import { getString, setString, removeKeys } from "/src/lib/storage.js?v=__RT_ASSET_V__";

export const FONT_STEPS = [17, 19, 22];
export const LINE_STEPS = [1.6, 1.75, 1.9];
/** @type {readonly string[]} */
export const THEMES = ["auto", "light", "dark", "paper"];

const KEYS = {
  fontPx: "fontPx",
  lineHeight: "lineHeight",
  theme: "theme",
  fontFamily: "fontFamily",
};

const SANS_STACK =
  '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Noto Sans CJK SC", "Microsoft YaHei", sans-serif';
const SERIF_STACK =
  '"Source Han Serif SC", "Noto Serif CJK SC", "Songti SC", "SimSun", Georgia, serif';

/**
 * @typedef {object} TypoState
 * @property {number|null} fontPx
 * @property {number|null} lineHeight
 * @property {string} theme
 * @property {string|null} fontFamily
 */

/** @type {TypoState} */
const state = {
  fontPx: null,
  lineHeight: null,
  theme: "auto",
  fontFamily: null,
};

/** @type {any} */
let boot = {};

/**
 * 启动时读一次 localStorage 并应用。boot 提供缺省值（defaultFontPx / defaultTheme）。
 * @param {any} bootConfig
 */
export function initTypography(bootConfig) {
  boot = bootConfig || {};
  const px = Number(getString(KEYS.fontPx, ""));
  state.fontPx = FONT_STEPS.includes(px) ? px : null;

  const lh = Number(getString(KEYS.lineHeight, ""));
  state.lineHeight = LINE_STEPS.includes(lh) ? lh : null;

  const theme = getString(KEYS.theme, "") || boot.defaultTheme || "auto";
  state.theme = THEMES.includes(theme) ? theme : "auto";

  const fam = getString(KEYS.fontFamily, "");
  state.fontFamily = fam === "serif" ? "serif" : null;

  applyAll();
}

/** 把当前状态写到 DOM。 */
export function applyAll() {
  applyTheme(state.theme);

  const view = document.getElementById("view");
  if (!view) return; // 登录页还没有 #view

  // 用户显式选过才写 inline：没选过就留给 tokens.css 的默认值与 @container 的窄窗降级
  if (state.fontPx) view.style.setProperty("--font-px", `${state.fontPx}px`);
  else view.style.removeProperty("--font-px");

  if (state.lineHeight) view.style.setProperty("--line-height", String(state.lineHeight));
  else view.style.removeProperty("--line-height");

  if (state.fontFamily === "serif") view.style.setProperty("--font-family", SERIF_STACK);
  else view.style.removeProperty("--font-family");
}

/**
 * 切主题。主题挂在 :root 上（不只是 #view），这样顶栏、遮罩一起变。
 * @param {string} theme
 */
export function applyTheme(theme) {
  const root = document.documentElement;
  root.dataset.theme = THEMES.includes(theme) ? theme : "auto";
}

/** 当前生效的字号（没选过就是 boot 的默认值）。 */
function effectiveFontPx() {
  if (state.fontPx) return state.fontPx;
  const d = Number(boot.defaultFontPx);
  return FONT_STEPS.includes(d) ? d : 19;
}

/** 当前生效的行距。 */
function effectiveLineHeight() {
  return state.lineHeight || 1.75;
}

/** @param {keyof TypoState} key @param {any} value */
function persist(key, value) {
  if (value === null || value === "") removeKeys([KEYS[key]]);
  else setString(KEYS[key], String(value));
  applyAll();
}

/** @type {HTMLElement|null} */
let sheetEl = null;
/** @type {HTMLElement|null} */
let lastFocused = null;

/** 打开排版设置面板（底栏抽屉样式）。 */
export function openTypographySheet() {
  if (sheetEl) return;

  lastFocused = /** @type {HTMLElement|null} */ (document.activeElement);

  const mask = document.createElement("div");
  mask.className = "sheet-mask";

  const sheet = document.createElement("div");
  sheet.className = "sheet";
  sheet.setAttribute("role", "dialog");
  sheet.setAttribute("aria-modal", "true");
  sheet.setAttribute("aria-label", "阅读排版设置");

  const handle = document.createElement("div");
  handle.className = "sheet-handle";

  const title = document.createElement("h2");
  title.className = "sheet-title";
  title.textContent = "阅读排版";

  sheet.append(handle, title);
  sheet.append(
    group("字号", "fontPx", [
      { value: 17, label: "小" },
      { value: 19, label: "中" },
      { value: 22, label: "大" },
    ], effectiveFontPx()),
    group("行距", "lineHeight", [
      { value: 1.6, label: "紧凑" },
      { value: 1.75, label: "标准" },
      { value: 1.9, label: "宽松" },
    ], effectiveLineHeight()),
    group("主题", "theme", [
      { value: "auto", label: "跟随系统" },
      { value: "light", label: "浅色" },
      { value: "dark", label: "深色" },
      { value: "paper", label: "护眼" },
    ], state.theme),
    group("字体", "fontFamily", [
      { value: null, label: "系统默认" },
      { value: "serif", label: "宋体" },
    ], state.fontFamily),
  );

  const foot = document.createElement("div");
  foot.className = "sheet-foot";

  const preview = document.createElement("p");
  preview.className = "sheet-preview";
  preview.textContent = "读书百遍，其义自见。The quick brown fox jumps over the lazy dog.";
  preview.style.fontSize = `${effectiveFontPx()}px`;
  preview.style.lineHeight = String(effectiveLineHeight());
  if (state.fontFamily === "serif") preview.style.fontFamily = SERIF_STACK;

  const reset = document.createElement("button");
  reset.type = "button";
  reset.className = "btn-ghost sheet-reset";
  reset.textContent = "恢复默认";
  reset.addEventListener("click", () => {
    state.fontPx = null;
    state.lineHeight = null;
    state.fontFamily = null;
    state.theme = "auto";
    removeKeys([KEYS.fontPx, KEYS.lineHeight, KEYS.fontFamily, KEYS.theme]);
    applyAll();
    refreshActive();
    refreshPreview();
  });

  foot.append(preview, reset);
  sheet.append(foot);

  const closeBtn = document.createElement("button");
  closeBtn.type = "button";
  closeBtn.className = "sheet-done";
  closeBtn.textContent = "完成";
  closeBtn.addEventListener("click", closeSheet);
  sheet.append(closeBtn);

  mask.addEventListener("click", closeSheet);
  document.addEventListener("keydown", onKeyDown, true);

  document.body.append(mask, sheet);
  sheetEl = sheet;

  requestAnimationFrame(() => {
    mask.classList.add("is-open");
    sheet.classList.add("is-open");
  });
  // 焦点落在「当前生效」的那一档上，而不是第一个按钮：
  // 键盘用户打开面板按 Tab 时，从当前值继续，而不是从头开始。
  /** @type {HTMLElement|null} */ (sheet.querySelector(".seg-btn.is-active"))?.focus({ preventScroll: true });

  /** 刷新高亮 */
  function refreshActive() {
    sheet.querySelectorAll(".seg").forEach((seg) => {
      const key = /** @type {HTMLElement} */ (seg).dataset.key;
      const cur = currentValue(/** @type {string} */ (key));
      seg.querySelectorAll(".seg-btn").forEach((btn) => {
        const v = /** @type {HTMLElement} */ (btn).dataset.value ?? "";
        const active = String(cur === null ? "" : cur) === v;
        /** @type {HTMLElement} */ (btn).classList.toggle("is-active", active);
        /** @type {HTMLElement} */ (btn).setAttribute("aria-pressed", active ? "true" : "false");
      });
    });
  }

  function refreshPreview() {
    preview.style.fontSize = `${effectiveFontPx()}px`;
    preview.style.lineHeight = String(effectiveLineHeight());
    preview.style.fontFamily = state.fontFamily === "serif" ? SERIF_STACK : "";
  }

  /** @param {string} key */
  function currentValue(key) {
    if (key === "fontPx") return effectiveFontPx();
    if (key === "lineHeight") return effectiveLineHeight();
    if (key === "theme") return state.theme;
    if (key === "fontFamily") return state.fontFamily;
    return null;
  }

  /**
   * 一行分段控件。
   * @param {string} label
   * @param {keyof TypoState} key
   * @param {{value:any,label:string}[]} options
   * @param {any} active
   */
  function group(label, key, options, active) {
    const box = document.createElement("div");
    box.className = "sheet-group";

    const name = document.createElement("div");
    name.className = "sheet-label";
    name.textContent = label;

    const seg = document.createElement("div");
    seg.className = "seg";
    seg.dataset.key = key;
    seg.setAttribute("role", "group");
    seg.setAttribute("aria-label", label);

    for (const opt of options) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "seg-btn";
      btn.dataset.value = opt.value === null ? "" : String(opt.value);
      btn.textContent = opt.label;
      const isActive = String(active === null ? "" : active) === btn.dataset.value;
      btn.classList.toggle("is-active", isActive);
      btn.setAttribute("aria-pressed", isActive ? "true" : "false");
      btn.addEventListener("click", () => {
        if (key === "fontFamily") {
          state.fontFamily = opt.value === "serif" ? "serif" : null;
          persist("fontFamily", state.fontFamily);
        } else if (key === "theme") {
          state.theme = String(opt.value);
          persist("theme", state.theme);
        } else if (key === "fontPx") {
          const v = Number(opt.value);
          state.fontPx = FONT_STEPS.includes(v) ? v : null;
          persist("fontPx", state.fontPx);
        } else if (key === "lineHeight") {
          const v = Number(opt.value);
          state.lineHeight = LINE_STEPS.includes(v) ? v : null;
          persist("lineHeight", state.lineHeight);
        }
        refreshActive();
        refreshPreview();
      });
      seg.append(btn);
    }

    box.append(name, seg);
    return box;
  }

  /** @param {KeyboardEvent} ev */
  function onKeyDown(ev) {
    if (ev.key === "Escape") {
      ev.preventDefault();
      closeSheet();
    }
  }

  function closeSheet() {
    document.removeEventListener("keydown", onKeyDown, true);
    const s = sheetEl;
    sheetEl = null;
    if (!s) return;
    const m = /** @type {HTMLElement|null} */ (document.querySelector(".sheet-mask"));
    s.classList.remove("is-open");
    m?.classList.remove("is-open");
    window.setTimeout(() => {
      s.remove();
      m?.remove();
    }, 200);
    lastFocused?.focus?.();
  }
}
