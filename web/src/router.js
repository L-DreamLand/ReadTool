// @ts-check
/**
 * 路由（History API，零依赖）。
 * 路由表见设计规格 §6.1：
 *   /                首页（分类网格）
 *   /c/<分类>         分类页
 *   /doc/<路径>       阅读页
 *   /login           登录
 *   /bookmarks       书签列表（FR-11）
 *   /recent          最近阅读（FR-12）
 *   /search?q=       全文搜索（FR-09）
 */

/**
 * @typedef {{name:string, params:Record<string,string>, url:string}} Route
 */

/** @type {string} */
let currentURL = "";

/**
 * @param {string} pathname
 * @returns {Route}
 */
export function parse(pathname) {
  const url = pathname || "/";
  if (url === "/" || url === "") return { name: "home", params: {}, url };

  if (url === "/login") return { name: "login", params: {}, url };

  if (url === "/bookmarks") return { name: "bookmarks", params: {}, url };

  if (url === "/recent") return { name: "recent", params: {}, url };

  if (url === "/search") return { name: "search", params: {}, url };

  if (url.startsWith("/c/")) {
    const raw = url.slice(3);
    return { name: "category", params: { category: safeDecode(raw) }, url };
  }

  if (url.startsWith("/doc/")) {
    const raw = url.slice(5);
    const path = raw
      .split("/")
      .filter((s) => s !== "")
      .map((s) => safeDecode(s))
      .join("/");
    return { name: "reader", params: { path }, url };
  }

  return { name: "notfound", params: {}, url };
}

/** @param {string} s */
function safeDecode(s) {
  try {
    return decodeURIComponent(s);
  } catch (e) {
    return s;
  }
}

/** 条目路径 → 站内 URL（逐段编码，保留斜杠） */
export function docHref(/** @type {string} */ path) {
  return "/doc/" + path.split("/").map(encodeURIComponent).join("/");
}

/** 分类名 → 站内 URL */
export function catHref(/** @type {string} */ name) {
  return "/c/" + encodeURIComponent(name);
}

/**
 * @param {string} to
 * @param {{replace?: boolean}} [opts]
 */
export function navigate(to, opts = {}) {
  if (to === location.pathname + location.search && !opts.replace) return;
  if (opts.replace) history.replaceState(null, "", to);
  else history.pushState(null, "", to);
  dispatch();
}

/** 当前路由（供视图读取） */
export function current() {
  return parse(location.pathname);
}

/** @type {Set<(r:Route)=>void>} */
const listeners = new Set();

/** @param {(r:Route)=>void} cb */
export function onRouteChange(cb) {
  listeners.add(cb);
}

function dispatch() {
  const url = location.pathname;
  if (url === currentURL) return;
  currentURL = url;
  const route = parse(url);
  for (const cb of listeners) {
    try {
      cb(route);
    } catch (err) {
      console.error("[router] 视图抛错", err);
    }
  }
}

/** 启动：接管 popstate 与站内链接点击 */
export function start() {
  currentURL = "";
  window.addEventListener("popstate", dispatch);

  document.addEventListener("click", (e) => {
    const target = /** @type {HTMLElement|null} */ (e.target instanceof Element ? e.target.closest("a") : null);
    if (!target) return;
    const href = target.getAttribute("href");
    if (!href) return;
    // 外链、新窗口、下载、锚点一律交给浏览器
    if (target.target === "_blank" || target.hasAttribute("download")) return;
    if (href.startsWith("http") || href.startsWith("//") || href.startsWith("mailto:")) return;
    if (!href.startsWith("/")) return;
    if (href.startsWith("/api/")) return;
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;

    e.preventDefault();
    navigate(href);
  });

  dispatch();
}

/**
 * 渲染后把 #view 滚回顶部（路由切换时）。
 * 阅读页自己负责恢复进度，所以由调用方决定是否调用。
 */
export function scrollTop() {
  const view = document.getElementById("view");
  if (view) view.scrollTop = 0;
}
