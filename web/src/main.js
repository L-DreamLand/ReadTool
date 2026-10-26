// @ts-check
/**
 * ReadTool 前端入口（零构建，ADR-015）。
 *
 * 职责：应用 boot 配置 → 登录门 → 路由分发。
 * boot 配置由 Go 在 index.html 内联注入，不额外发请求（设计规格 §5.1）。
 */

import { start, onRouteChange, current, navigate } from "/src/router.js?v=__RT_ASSET_V__";
import { api, ApiError } from "/src/api.js?v=__RT_ASSET_V__";
import {
  initShell,
  resetTopbar,
  setTopbar,
  setRescanHandler,
  setRescanBusy,
  flashRescanDiff,
} from "/src/components/AppShell.js?v=__RT_ASSET_V__";
import { initTypography } from "/src/components/Typography.js?v=__RT_ASSET_V__";
import { renderHome } from "/src/views/Home.js?v=__RT_ASSET_V__";
import { renderCategory } from "/src/views/Category.js?v=__RT_ASSET_V__";
import { renderReader } from "/src/views/Reader.js?v=__RT_ASSET_V__";
import { renderLogin } from "/src/views/Login.js?v=__RT_ASSET_V__";
import { renderBookmarks } from "/src/views/Bookmarks.js?v=__RT_ASSET_V__";
import { renderRecent } from "/src/views/Recent.js?v=__RT_ASSET_V__";
import { renderSearch } from "/src/views/Search.js?v=__RT_ASSET_V__";
import { errorBox } from "/src/lib/dom.js?v=__RT_ASSET_V__";

/**
 * @typedef {{version?:number, dev?:boolean, authenticated?:boolean, needSetup?:boolean,
 *   deviceLabel?:string, minCellPx?:number, cardViewPx?:number, drawerPx?:number,
 *   defaultFontPx?:number, contentMaxCh?:number, defaultTheme?:string}} Boot
 */

// 供 index.html 里的看门狗判断「脚本真的跑起来了」
window.__RT_BOOTED__ = true;

const rawBoot = /** @type {any} */ (window).__RT_BOOT__;
const boot = /** @type {Boot} */ (rawBoot != null ? rawBoot : {});
const view = /** @type {HTMLElement} */ (document.getElementById("view"));

/** @type {(() => void)|undefined} */
let cleanup;
/** 防连点：扫描期间再来一次直接忽略 */
let rescanRunning = false;

applyBoot(boot);
initShell();
setRescanHandler(() => {
  void onRescan();
});

// 用户手动选过字号就用用户的；没选过就不写 inline，
// 让 @container 的窄窗默认值（17px）与 boot 的 19px 各自生效
initTypography(boot);

if (!boot.authenticated) {
  renderLogin(view, { needSetup: Boolean(boot.needSetup) });
} else {
  // 先注册监听再 start()：start 会立即 dispatch 一次首个路由
  onRouteChange((route) => {
    void show(route);
  });
  start();
}

/**
 * @param {import("./router.js").Route} route
 */
async function show(route) {
  if (cleanup) {
    try {
      cleanup();
    } catch (e) {
      /* 清理失败不影响切换 */
    }
    cleanup = undefined;
  }

  resetTopbar();
  const controller = new AbortController();
  const ctx = { signal: controller.signal };

  try {
    if (route.name === "home") {
      await renderHome(view, ctx);
      view.scrollTop = 0;
    } else if (route.name === "category") {
      await renderCategory(view, { category: route.params.category || "" }, ctx);
      view.scrollTop = 0;
    } else if (route.name === "reader") {
      // 阅读页自己恢复进度，不要在这里置零
      const docPath = route.params.path || "";
      try {
        cleanup = (await renderReader(view, { path: docPath }, ctx)) || undefined;
      } catch (err) {
        // 篇目被删/改名：单独给一条出路，别落到通用错误页（FR-21 规则 7）
        if (err instanceof ApiError && err.code === "kb.path_not_found") {
          renderVanished(docPath);
          return;
        }
        throw err;
      }
    } else if (route.name === "bookmarks") {
      await renderBookmarks(view, ctx);
      view.scrollTop = 0;
    } else if (route.name === "recent") {
      await renderRecent(view, ctx);
      view.scrollTop = 0;
    } else if (route.name === "search") {
      const q = new URLSearchParams(location.search).get("q") || "";
      await renderSearch(view, q, ctx);
      view.scrollTop = 0;
    } else if (route.name === "login") {
      resetTopbar();
      renderLogin(view, { needSetup: Boolean(boot.needSetup) });
    } else {
      setTopbar({ title: "找不到页面", backTo: "/" });
      view.textContent = "";
      view.append(
        errorBox("页面不存在", `地址 ${route.url} 没有对应的页面。知识库里的链接可能已经失效。`),
      );
    }
  } catch (err) {
    handleError(err);
  }
}

/**
 * 顶栏「重新扫描」（FR-21）。
 *
 * 成功后走 show(current()) 重绘同一个路由 —— 目录、上下篇、抽屉都靠这一条路径刷新，
 * 阅读页的滚动位置由它自己的进度恢复逻辑保住（设计规格 §5.6 不变式 4）。
 */
async function onRescan() {
  if (rescanRunning) return;
  rescanRunning = true;
  setRescanBusy(true);
  try {
    const diff = /** @type {{added:number, removed:number, changed:number}} */ (await api.rescan());
    flashRescanDiff(diff);
    await show(current());
  } catch (err) {
    // 5 秒内连点：静默即可，按钮自己会恢复
    if (err instanceof ApiError && err.code === "rate_limited") return;
    if (err instanceof ApiError && err.code === "kb.scan_failed") {
      console.error("[readtool] 重新扫描失败", err);
      setTopbar({ title: "扫描失败", backTo: "/" });
      view.textContent = "";
      view.append(
        errorBox("重新扫描失败", `${err.message}。原有的目录仍然可用，可以稍后再试。`, [
          { label: "再试一次", onClick: () => void onRescan() },
          { label: "回到首页", onClick: () => navigate("/") },
        ]),
      );
      return;
    }
    handleError(err);
  } finally {
    rescanRunning = false;
    setRescanBusy(false);
  }
}

/**
 * 篇目在磁盘上已经不存在（被删、改名、移动）。
 * @param {string} docPath
 */
function renderVanished(docPath) {
  const category = docPath.split("/")[0] || "";
  setTopbar({
    title: "这篇已不存在",
    backTo: category ? `/c/${encodeURIComponent(category)}` : "/",
  });
  view.textContent = "";
  view.append(
    errorBox(
      "这篇已经不在了",
      `「${docPath}」在知识库里找不到了——可能被移动、改名或删除。点「重新扫描」更新目录，或回到分类里另找一篇。`,
      [
        { label: "重新扫描目录", onClick: () => void onRescan() },
        { label: "回到首页", onClick: () => navigate("/") },
      ],
    ),
  );
}

/** @param {unknown} err */
function handleError(err) {
  if (err instanceof DOMException && err.name === "AbortError") return;

  if (err instanceof ApiError && err.status === 401) {
    resetTopbar();
    renderLogin(view, { needSetup: false, message: "会话已过期，请重新输入口令。" });
    return;
  }

  console.error("[readtool] 渲染失败", err);
  setTopbar({ title: "出错了", backTo: "/" });
  view.textContent = "";
  view.append(
    errorBox(
      "页面出错了",
      err instanceof Error ? err.message : String(err),
    ),
  );
}

/**
 * 把 boot 配置写进 CSS 变量与 <html data-*>（阈值留给宽表判定用）。
 * @param {Boot} b
 */
function applyBoot(b) {
  const root = document.documentElement;

  if (b.defaultFontPx) root.style.setProperty("--font-px", `${b.defaultFontPx}px`);
  if (b.contentMaxCh) root.style.setProperty("--content-max-ch", String(b.contentMaxCh));
  if (b.drawerPx) root.style.setProperty("--drawer-w", `${b.drawerPx}px`);

  if (b.minCellPx) root.dataset.minCellPx = String(b.minCellPx);
  if (b.cardViewPx) root.dataset.cardViewPx = String(b.cardViewPx);
  if (b.defaultTheme) root.dataset.theme = b.defaultTheme;
  else root.dataset.theme = "auto";
}

// 站内任何 fetch 拿到 401，都回到登录门（令牌过期、服务重启后密钥轮换等）
window.addEventListener("unhandledrejection", (e) => {
  if (e.reason instanceof ApiError && e.reason.status === 401) {
    e.preventDefault();
    resetTopbar();
    renderLogin(view, { needSetup: false, message: "会话已过期，请重新输入口令。" });
  }
});

// 供调试：控制台里能看当前路由
Object.defineProperty(window, "__RT__", {
  value: {
    boot,
    route: () => current(),
    go: (/** @type {string} */ to) => navigate(to),
  },
  configurable: true,
});
