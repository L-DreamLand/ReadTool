// @ts-check
/**
 * 阅读进度与最近阅读。
 *
 * 分两层（FR-10 / FR-12）：
 *   - 本地（localStorage）：秒开、离线也有、恢复位置不等网络；
 *   - 服务端（/api/state/*）：跨设备同步，最近阅读的权威数据源。
 * 本地永远只是缓存：服务端有更新的记录时以服务端为准。
 */

import { getJSON, setJSON } from "/src/lib/storage.js?v=__RT_ASSET_V__";
import { api } from "/src/api.js?v=__RT_ASSET_V__";

const RECENT = "recent";
const MAX_RECENT = 12;

/** 上报服务端的最小间隔：滚动时每 400ms 存本地，但最多每 4 秒打一次网络 */
const SERVER_GAP_MS = 4000;

/**
 * @typedef {{path:string, name:string, category:string, at:number, ratio:number}} RecentItem
 */

const PROGRESS_PREFIX = "progress.";

/**
 * 记录一篇的阅读比例（0–1），只写本地缓存。
 * @param {string} path
 * @param {{name?:string, category?:string}} meta
 * @param {number} ratio
 */
export function saveProgress(path, meta, ratio) {
  const r = Math.max(0, Math.min(1, Number.isFinite(ratio) ? ratio : 0));
  setJSON(PROGRESS_PREFIX + path, { ratio: r, at: Date.now() });
  if (r >= 0.999) {
    // 读完了也算"最近阅读"，但把比例修正为 1
    pushRecent(path, meta, 1);
  } else {
    pushRecent(path, meta, r);
  }
}

/**
 * @param {string} path
 * @returns {{ratio:number, at:number}|null}
 */
export function getProgress(path) {
  const v = getJSON(PROGRESS_PREFIX + path, null);
  if (!v || typeof v.ratio !== "number") return null;
  return v;
}

/**
 * 从服务端读进度；没记录过（404）返回 null，网络错误也返回 null（阅读不受影响）。
 * @param {string} path
 * @param {AbortSignal} [signal]
 * @returns {Promise<{scrollPct:number, anchorId?:string, device?:string, updatedAt:number}|null>}
 */
export async function fetchServerProgress(path, signal) {
  try {
    const v = await api.progress(path, signal);
    if (!v || typeof v.scrollPct !== "number") return null;
    return v;
  } catch (e) {
    return null;
  }
}

/**
 * 上报服务端。fire-and-forget：失败只在本地留痕，绝不打断阅读。
 * @param {string} path
 * @param {number} ratio
 * @param {string} [anchorId]
 */
export async function pushProgress(path, ratio, anchorId) {
  try {
    await api.putProgress({ path, scrollPct: ratio, anchorId: anchorId || "" });
  } catch (e) {
    // 401/网络错误都静默：进度本来就有本地副本
  }
}

/**
 * @param {string} path
 * @param {{name?:string, category?:string}} meta
 * @param {number} ratio
 */
function pushRecent(path, meta, ratio) {
  /** @type {RecentItem[]} */
  const list = getJSON(RECENT, []);
  const next = list.filter((it) => it.path !== path);
  next.unshift({
    path,
    name: meta.name || path,
    category: meta.category || "",
    at: Date.now(),
    ratio,
  });
  setJSON(RECENT, next.slice(0, MAX_RECENT));
}

/**
 * 本地最近阅读（服务端不可达时的兜底）。
 * @param {number} [limit]
 * @returns {RecentItem[]}
 */
export function listRecent(limit = 6) {
  return getJSON(RECENT, []).slice(0, limit);
}

/**
 * 中途离开也要记住进度：滚动/切后台/关页面都写一次。
 * 本地 400ms 节流，服务端 4s 节流 + 离开时强制补一次。
 *
 * @param {HTMLElement} view 滚动容器
 * @param {string} path
 * @param {() => {name?:string, category?:string}} metaFn
 * @param {() => string} [anchorFn] 当前所在标题的 id（没有则为空串 = 条目级）
 * @returns {() => void} 取消监听的函数
 */
export function bindProgressAutosave(view, path, metaFn, anchorFn) {
  /** @type {number|undefined} */
  let timer;
  /** @type {number} */
  let lastServer = 0;

  const anchor = () => (anchorFn ? anchorFn() : "");

  const flush = (/** @type {boolean} */ force) => {
    const ratio = readRatio(view);
    saveProgress(path, metaFn(), ratio);
    const now = Date.now();
    if (!force && now - lastServer < SERVER_GAP_MS) return;
    lastServer = now;
    void pushProgress(path, ratio, anchor());
  };

  const onScroll = () => {
    if (timer !== undefined) return;
    timer = window.setTimeout(() => {
      timer = undefined;
      flush(false);
    }, 400);
  };
  const onHide = () => {
    if (document.visibilityState === "hidden") flush(true);
  };
  const onPageHide = () => flush(true);

  view.addEventListener("scroll", onScroll, { passive: true });
  document.addEventListener("visibilitychange", onHide);
  window.addEventListener("pagehide", onPageHide);

  return () => {
    view.removeEventListener("scroll", onScroll);
    document.removeEventListener("visibilitychange", onHide);
    window.removeEventListener("pagehide", onPageHide);
    if (timer !== undefined) {
      clearTimeout(timer);
      timer = undefined;
    }
    // 离开这篇（切换路由）时补一次，避免最后一段阅读丢失
    flush(true);
  };
}

/** @param {HTMLElement} view */
export function readRatio(view) {
  const max = view.scrollHeight - view.clientHeight;
  if (max <= 0) return 1;
  return view.scrollTop / max;
}
