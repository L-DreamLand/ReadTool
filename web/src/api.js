// @ts-check
/**
 * API 客户端（设计规格 §6）：统一错误体 → ApiError。
 * 所有响应都用 no-store，浏览器端不做 HTTP 缓存（FR-18）。
 */

export class ApiError extends Error {
  /**
   * @param {number} status
   * @param {string} code
   * @param {string} message
   * @param {string} [detail]
   */
  constructor(status, code, message, detail) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.detail = detail;
  }
}

/** 单请求最长等待：超过就当成「连不上」而不是一直转圈 */
const TIMEOUT_MS = 10000;

/**
 * @param {string} method
 * @param {string} path
 * @param {{params?: Record<string, string | number | undefined>, body?: unknown, signal?: AbortSignal}} [opts]
 */
async function request(method, path, opts = {}) {
  /** @type {string} */
  let url = path;
  if (opts.params) {
    const qs = new URLSearchParams();
    for (const [k, v] of Object.entries(opts.params)) {
      if (v !== undefined && v !== null) qs.set(k, String(v));
    }
    const s = qs.toString();
    if (s) url += `?${s}`;
  }

  // 超时与外部 signal 合并：谁先触发都算取消
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), TIMEOUT_MS);
  if (opts.signal) {
    if (opts.signal.aborted) ctl.abort();
    else opts.signal.addEventListener("abort", () => ctl.abort(), { once: true });
  }

  /** @type {RequestInit} */
  const init = {
    method,
    headers: { Accept: "application/json" },
    signal: ctl.signal,
    credentials: "same-origin",
  };
  if (opts.body !== undefined) {
    init.headers = { ...init.headers, "Content-Type": "application/json" };
    init.body = JSON.stringify(opts.body);
  }

  let resp;
  try {
    resp = await fetch(url, init);
  } catch (e) {
    clearTimeout(timer);
    const aborted = e && e.name === "AbortError";
    throw new ApiError(
      0,
      aborted ? "network.timeout" : "network.unreachable",
      aborted
        ? "服务没有在 10 秒内响应。请确认 PC 上 ReadTool 还在运行，且本机与 PC 在同一网络（Wi-Fi 或 EasyTier）。"
        : "连不上服务。请确认 PC 上 ReadTool 还在运行，地址没有变。",
    );
  }
  clearTimeout(timer);
  const text = await resp.text();
  /** @type {any} */
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch (e) {
      data = null;
    }
  }

  if (!resp.ok) {
    const err = data && data.error ? data.error : {};
    throw new ApiError(
      resp.status,
      err.code || `http.${resp.status}`,
      err.message || `请求失败（HTTP ${resp.status}）`,
      err.detail,
    );
  }
  return data;
}

export const api = {
  /** 健康状态：唯一免鉴权接口 */
  health: (/** @type {AbortSignal} */ signal) => request("GET", "/api/health", { signal }),

  /** 当前会话；永远 200 */
  session: (/** @type {AbortSignal} */ signal) => request("GET", "/api/auth/session", { signal }),

  /** 首次调用即「设置访问口令」，之后是登录 */
  login: (/** @type {string} */ password) => request("POST", "/api/auth/login", { body: { password } }),

  logout: () => request("POST", "/api/auth/logout", { body: {} }),

  /** 知识库元信息 */
  meta: (/** @type {AbortSignal} */ signal) => request("GET", "/api/kb/meta", { signal }),

  /** 分类树 + 条目（一次取全集，34 条量级足够） */
  catalog: (/** @type {AbortSignal} */ signal) => request("GET", "/api/kb/catalog", { signal }),

  /** 单篇渲染结果 */
  doc: (/** @type {string} */ path, /** @type {AbortSignal} */ signal) =>
    request("GET", "/api/kb/doc", { params: { path }, signal }),

  /**
   * 全文搜索（FR-09）。snippet 里的 <em> 是服务端加的，前端按标签切分渲染（见 Search.js）。
   * @param {string} q
   * @param {{category?:string, titleOnly?:boolean, limit?:number}} [opts]
   */
  search: (
    /** @type {string} */ q,
    /** @type {{category?:string, titleOnly?:boolean, limit?:number}} */ opts = {},
    /** @type {AbortSignal} */ signal,
  ) => {
    /** @type {Record<string,string|number>} */
    const params = { q };
    if (opts.category) params.category = opts.category;
    if (opts.titleOnly) params.title_only = 1;
    if (opts.limit) params.limit = opts.limit;
    return request("GET", "/api/search", { params, signal });
  },

  /** 手动重新扫描知识库（FR-21）；返回差异计数 */
  rescan: () => request("POST", "/api/kb/rescan", { body: {} }),

  /** 读某条目的阅读进度；没读过 → 404 state.not_found（调用方按"无进度"处理） */
  progress: (/** @type {string} */ path, /** @type {AbortSignal} */ signal) =>
    request("GET", "/api/state/progress", { params: { path }, signal }),

  /** 批量读进度（首页用它一次性问最近几条） */
  progressMany: (/** @type {string[]} */ paths, /** @type {AbortSignal} */ signal) =>
    request("GET", "/api/state/progress", { params: { paths: paths.join(",") }, signal }),

  /** 写进度（跨设备同步，FR-10）。scrollPct 由服务端再夹一次 0–1 */
  putProgress: (/** @type {{path:string, scrollPct:number, anchorId?:string}} */ body) =>
    request("PUT", "/api/state/progress", { body }),

  /** 最近阅读（FR-12）：服务端按时间倒序，已删除的条目会被过滤 */
  recent: (/** @type {number} */ limit, /** @type {AbortSignal} */ signal) =>
    request("GET", "/api/state/recent", { params: { limit }, signal }),

  /** 全部书签（FR-11） */
  bookmarks: (/** @type {AbortSignal} */ signal) => request("GET", "/api/state/bookmarks", { signal }),

  addBookmark: (/** @type {{path:string, anchorId?:string, anchorText?:string}} */ body) =>
    request("POST", "/api/state/bookmarks", { body }),

  delBookmark: (/** @type {string} */ id) =>
    request("DELETE", "/api/state/bookmarks/" + encodeURIComponent(id)),

  /** 原始 Markdown 地址（用于「查看原文」链接） */
  /**
   * 原始 Markdown 地址。
   * @param {string} path
   * @param {{download?: boolean}} [opts] download=true 时服务端返回 attachment（存成 .md）
   */
  rawURL: (path, opts) =>
    `/api/kb/raw?path=${encodeURIComponent(path)}${opts && opts.download ? "&download=1" : ""}`,
};
