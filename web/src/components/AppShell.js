// @ts-check
/**
 * 应用骨架：顶栏动作、抽屉实例、返回按钮、主题与字号（FR-05/FR-15）。
 */

import { NavDrawer } from "/src/components/NavDrawer.js?v=__RT_ASSET_V__";
import { navigate } from "/src/router.js?v=__RT_ASSET_V__";
import { openTypographySheet } from "/src/components/Typography.js?v=__RT_ASSET_V__";

/** @type {HTMLElement} */
let topbarTitle;
/** @type {HTMLButtonElement} */
let backBtn;
/** @type {HTMLElement} */
let actionsEl;
/** @type {HTMLButtonElement} */
let tocBtn;
/** @type {HTMLButtonElement} */
let fab;
/** @type {NavDrawer} */
export let drawer;

/**
 * 顶栏「重新扫描」按钮（FR-21）。元素常驻，靠挂载/摘除控制出现，
 * 这样忙碌态（转圈）与差异提示（+3 −1 ~2）不需要重建节点。
 * @type {HTMLButtonElement}
 */
let rescanBtn;
/** @type {(() => void)|undefined} */
let rescanHandler;
/** @type {number|undefined} */
let rescanTimer;

/**
 * 顶栏「书签」按钮（FR-11）。同样常驻，★/☆ 只换字形与 aria-pressed。
 * @type {HTMLButtonElement}
 */
let bookmarkBtn;
/** @type {(() => void)|undefined} */
let bookmarkHandler;

/**
 * 顶栏「搜索」按钮（FR-09）。阅读页不挂（顶栏已经 5 个按钮），改由目录抽屉里的
 * 「搜索全文」进入 —— 见 NavDrawer 的 tool 行。
 * @type {HTMLButtonElement}
 */
let searchBtn;

/**
 * @typedef {{icon:string, label:string, onClick:()=>void, className?:string}} Action
 */

export function initShell() {
  topbarTitle = /** @type {HTMLElement} */ (document.getElementById("topbar-title"));
  backBtn = /** @type {HTMLButtonElement} */ (document.getElementById("btn-back"));
  actionsEl = /** @type {HTMLElement} */ (document.getElementById("topbar-actions"));
  tocBtn = /** @type {HTMLButtonElement} */ (document.getElementById("btn-toc"));
  fab = /** @type {HTMLButtonElement} */ (document.getElementById("btn-drawer-fab"));

  const drawerEl = /** @type {HTMLElement} */ (document.getElementById("drawer"));
  const mask = /** @type {HTMLElement} */ (document.getElementById("drawer-mask"));
  drawer = new NavDrawer(drawerEl, mask);

  tocBtn.addEventListener("click", () => drawer.toggle());
  fab.addEventListener("click", () => drawer.toggle());

  backBtn.addEventListener("click", () => {
    if (history.length > 1) history.back();
    else navigate("/");
  });

  bookmarkBtn = document.createElement("button");
  bookmarkBtn.type = "button";
  bookmarkBtn.className = "icon-btn bookmark-btn";
  bookmarkBtn.textContent = "☆";
  bookmarkBtn.title = "加书签";
  bookmarkBtn.setAttribute("aria-label", "加书签");
  bookmarkBtn.setAttribute("aria-pressed", "false");
  bookmarkBtn.addEventListener("click", () => {
    if (bookmarkHandler) bookmarkHandler();
  });

  searchBtn = document.createElement("button");
  searchBtn.type = "button";
  searchBtn.className = "icon-btn search-btn";
  searchBtn.textContent = "🔍";
  searchBtn.title = "搜索知识库";
  searchBtn.setAttribute("aria-label", "搜索知识库");
  searchBtn.addEventListener("click", () => navigate("/search"));

  rescanBtn = document.createElement("button");
  rescanBtn.type = "button";
  rescanBtn.className = "icon-btn rescan-btn";
  rescanBtn.textContent = "⟳";
  rescanBtn.title = "重新扫描知识库";
  rescanBtn.setAttribute("aria-label", "重新扫描知识库");
  rescanBtn.addEventListener("click", () => {
    if (rescanHandler) rescanHandler();
  });

}

/** @param {() => void} fn */
export function setBookmarkHandler(fn) {
  bookmarkHandler = fn;
}

/**
 * 把 ☆ 挂到顶栏（只有阅读页调用）。顺序：⟳ ☆ A ◐，小屏也排得下。
 * @param {boolean} [enabled]
 */
export function mountBookmarkAction(enabled = true) {
  if (!enabled) {
    bookmarkBtn.remove();
    return;
  }
  actionsEl.prepend(bookmarkBtn);
}

/**
 * 切换实心/空心星：当前章节已收藏就是 ★。
 * @param {boolean} active
 */
export function setBookmarkActive(active) {
  bookmarkBtn.textContent = active ? "★" : "☆";
  bookmarkBtn.setAttribute("aria-pressed", active ? "true" : "false");
  bookmarkBtn.title = active ? "取消书签" : "加书签";
  bookmarkBtn.setAttribute("aria-label", active ? "取消书签" : "加书签");
  bookmarkBtn.classList.toggle("is-marked", Boolean(active));
}

/** @param {() => void} fn */
export function setRescanHandler(fn) {
  rescanHandler = fn;
}

/**
 * 把「重新扫描」挂到当前顶栏（每次 setTopbar 之后调用一次）。
 * 传 false 表示这个页面暂不提供（目前所有页面都提供）。
 * @param {boolean} [enabled]
 */
export function mountRescanAction(enabled = true) {
  if (!enabled) {
    rescanBtn.remove();
    return;
  }
  // prepend：让它排在「A / ◐」左边，小屏也不会被挤掉
  actionsEl.prepend(rescanBtn);
}

/**
 * 把 🔍 挂到当前顶栏（每次 setTopbar 之后调用一次）。
 * @param {boolean} [enabled]
 */
export function mountSearchAction(enabled = true) {
  if (!enabled) {
    searchBtn.remove();
    return;
  }
  actionsEl.prepend(searchBtn);
}

/** 扫描进行中：禁用并转圈 */
export function setRescanBusy(busy) {
  rescanBtn.disabled = Boolean(busy);
  rescanBtn.classList.toggle("is-busy", Boolean(busy));
}

/**
 * 扫完闪一下差异（+a −r ~c），3 秒后恢复成 ⟳。
 * @param {{added:number, removed:number, changed:number}} diff
 */
export function flashRescanDiff(diff) {
  const parts = [];
  if (diff.added > 0) parts.push(`+${diff.added}`);
  if (diff.removed > 0) parts.push(`−${diff.removed}`);
  if (diff.changed > 0) parts.push(`~${diff.changed}`);

  if (rescanTimer !== undefined) window.clearTimeout(rescanTimer);
  if (parts.length === 0) {
    // 零差异也要有反馈，否则用户会怀疑按钮没生效
    rescanBtn.textContent = "✓";
    rescanBtn.classList.add("flash");
    rescanBtn.title = "目录没有变化";
    rescanTimer = window.setTimeout(() => {
      rescanBtn.textContent = "⟳";
      rescanBtn.classList.remove("flash");
      rescanBtn.title = "重新扫描知识库";
    }, 1600);
    return;
  }
  rescanBtn.textContent = parts.join(" ");
  rescanBtn.classList.add("flash");
  rescanBtn.title = `新增 ${diff.added} · 删除 ${diff.removed} · 变更 ${diff.changed}`;
  rescanTimer = window.setTimeout(() => {
    rescanBtn.textContent = "⟳";
    rescanBtn.classList.remove("flash");
    rescanBtn.title = "重新扫描知识库";
  }, 3000);
}

/** 重置顶栏（每次渲染视图前调用） */
export function resetTopbar() {
  backBtn.hidden = true;
  tocBtn.hidden = true;
  fab.hidden = true;
  actionsEl.textContent = "";
}

/**
 * @param {{title?:string, backTo?:string, actions?:Action[], toc?:boolean, search?:boolean}} cfg
 */
export function setTopbar(cfg) {
  if (cfg.title !== undefined) topbarTitle.textContent = cfg.title;

  backBtn.hidden = !cfg.backTo;
  if (cfg.backTo) {
    // 用直接跳转而不是 history.back()：从外部直接打开某篇时后退会离开本站
    backBtn.onclick = () => navigate(/** @type {string} */ (cfg.backTo));
  }

  actionsEl.textContent = "";
  for (const action of cfg.actions || []) {
    const btn = document.createElement("button");
    btn.className = action.className ? `icon-btn ${action.className}` : "icon-btn";
    btn.type = "button";
    btn.textContent = action.icon;
    btn.title = action.label;
    btn.setAttribute("aria-label", action.label);
    btn.addEventListener("click", action.onClick);
    actionsEl.append(btn);
  }

  tocBtn.hidden = !cfg.toc;
  fab.hidden = !cfg.toc;

  // 「搜索」按页面决定（阅读页交给抽屉），默认都挂
  mountSearchAction(cfg.search !== false);

  // 「更新」按钮每个页面都在（FR-21 规则 1）
  mountRescanAction();
}

/** 顶栏参与阅读的常用动作：打开排版设置（字号/行距/主题/字体，FR-15） */
export function readingActions() {
  /** @type {Action[]} */
  const actions = [
    {
      icon: "Aa",
      label: "阅读排版设置",
      className: "type-btn",
      onClick: () => openTypographySheet(),
    },
  ];
  return actions;
}

/** 内容区快捷构造 */
export function viewEl() {
  return /** @type {HTMLElement} */ (document.getElementById("view"));
}
