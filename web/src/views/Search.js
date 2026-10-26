// @ts-check
/**
 * 全文搜索页（FR-09）。
 *
 * 交互：输入即搜（300ms 防抖）→ 结果按权重排序；支持分类过滤与「仅标题」。
 * 片段由服务端生成，只有 <em> 是标记，其余已转义 —— 这里按标签切分后用 textContent 塞文本，
 * 既不二次解析 HTML，也不会把转义实体显示成 `&amp;`。
 */

import { api } from "/src/api.js?v=__RT_ASSET_V__";
import { setTopbar } from "/src/components/AppShell.js?v=__RT_ASSET_V__";
import { docHref, navigate } from "/src/router.js?v=__RT_ASSET_V__";
import { getJSON, setJSON } from "/src/lib/storage.js?v=__RT_ASSET_V__";

const HISTORY_KEY = "searchHistory";
const HISTORY_MAX = 10;

/** @type {Promise<any>|undefined} 分类列表只取一次 */
let catalogPromise;

/**
 * @param {HTMLElement} view
 * @param {string} initialQuery
 * @param {{signal:AbortSignal}} ctx
 */
export async function renderSearch(view, initialQuery, ctx) {
  setTopbar({ title: "搜索", backTo: "/", search: false });

  /** @type {string} */
  let category = "";
  /** @type {boolean} */
  let titleOnly = false;
  /** @type {AbortController|undefined} */
  let inflight;
  /** @type {number|undefined} */
  let debounce;

  view.textContent = "";
  const wrap = el("div", "search");

  const form = /** @type {HTMLFormElement} */ (el("form", "search-form"));
  form.setAttribute("role", "search");
  const input = /** @type {HTMLInputElement} */ (el("input", "search-input"));
  input.type = "search";
  input.name = "q";
  input.placeholder = "搜索标题、正文、关键词";
  input.autocomplete = "off";
  input.value = initialQuery;
  input.setAttribute("aria-label", "搜索知识库");
  const submit = el("button", "search-submit", "搜索");
  submit.type = "submit";
  form.append(input, submit);

  const filters = el("div", "search-filters");
  const titleLabel = el("label", "search-toggle");
  const titleBox = /** @type {HTMLInputElement} */ (el("input", ""));
  titleBox.type = "checkbox";
  titleBox.id = "search-title-only";
  // 视觉上有「仅标题」三个字，但复选框自己没有可访问名（读屏会念成「复选框」）
  titleBox.setAttribute("aria-label", "仅在标题中搜索");
  titleLabel.append(titleBox, document.createTextNode(" 仅标题"));
  const chips = el("div", "chips");
  filters.append(titleLabel, chips);

  const hint = el("div", "search-hint");
  const results = el("div", "search-results");
  wrap.append(form, filters, hint, results);
  view.append(wrap);

  // 分类胶囊：只取一次
  const cats = await loadCategories(ctx).catch(() => []);
  renderChips(chips, cats, "");
  if (cats.length === 0) chips.hidden = true;

  form.addEventListener("submit", (e) => {
    e.preventDefault();
    void run(input.value, true);
  });

  input.addEventListener("input", () => {
    if (debounce !== undefined) window.clearTimeout(debounce);
    debounce = window.setTimeout(() => void run(input.value, false), 300);
  });

  titleBox.addEventListener("change", () => {
    titleOnly = titleBox.checked;
    void run(input.value, true);
  });

  function renderChips(host, list, active) {
    host.textContent = "";
    for (const name of list) {
      const btn = el("button", "chip" + (name === active ? " is-active" : ""), name);
      btn.type = "button";
      btn.addEventListener("click", () => {
        category = category === name ? "" : name;
        renderChips(host, list, category);
        void run(input.value, true);
      });
      host.append(btn);
    }
  }

  /**
   * @param {string} raw
   * @param {boolean} remember 是否记进历史（只有提交/点筛选才记，避免把逐字输入都存下来）
   */
  async function run(raw, remember) {
    const q = raw.trim();
    // URL 上留一份，方便刷新/分享
    const search = q ? "?q=" + encodeURIComponent(q) : "";
    history.replaceState(null, "", "/search" + search);

    if (inflight) inflight.abort();
    inflight = new AbortController();
    const signal = inflight.signal;

    if (q === "") {
      hint.hidden = false;
      results.textContent = "";
      await renderIdle(hint, results, signal);
      return;
    }

    hint.hidden = true;
    if (remember) pushHistory(q);

    try {
      const res = await api.search(q, { category, titleOnly, limit: 20 }, signal);
      if (signal.aborted) return;
      renderHits(results, res, q, () => {
        // 用户点了「不限分类再看看」
        category = "";
        renderChips(chips, cats, "");
        void run(q, false);
      });
    } catch (err) {
      if (signal.aborted) return;
      throw err;
    }
  }

  const startQ = initialQuery.trim();
  if (startQ) {
    await run(startQ, true).catch((err) => {
      hint.hidden = false;
      hint.textContent = "搜索失败：" + (err && err.message ? err.message : "未知错误");
    });
  } else {
    await renderIdle(hint, results, ctx.signal);
  }

  input.focus({ preventScroll: true });
}

/**
 * 空查询：历史 + 最近读过。
 * @param {HTMLElement} hint
 * @param {HTMLElement} results
 * @param {AbortSignal} signal
 */
async function renderIdle(hint, results, signal) {
  hint.textContent = "";
  results.textContent = "";

  const history = /** @type {string[]} */ (getJSON(HISTORY_KEY, []));
  if (Array.isArray(history) && history.length > 0) {
    const box = el("div", "idle-block");
    box.append(el("div", "idle-title", "最近搜过"));
    const row = el("div", "chips");
    for (const q of history.slice(0, HISTORY_MAX)) {
      const btn = el("button", "chip", q);
      btn.type = "button";
      btn.addEventListener("click", () => {
        navigate("/search?q=" + encodeURIComponent(q));
      });
      row.append(btn);
    }
    box.append(row);
    results.append(box);
  }

  let shown = false;
  const recent = await api.recent(5, signal).catch(() => null);
  if (recent && Array.isArray(recent.items) && recent.items.length > 0) {
    const box = el("div", "idle-block");
    box.append(el("div", "idle-title", "接着上次读"));
    for (const item of recent.items) {
      const a = /** @type {HTMLAnchorElement} */ (el("a", "entry-row"));
      a.href = docHref(item.path);
      a.append(el("span", "entry-name", item.name));
      a.append(el("span", "entry-meta", item.category));
      box.append(a);
    }
    results.append(box);
    shown = true;
  }

  if (!shown) {
    // 既没有历史也没有阅读记录：给一句最基础的用法说明，别留白
    hint.textContent = "输入关键词搜索知识库：标题、正文、摘要与关键词都会参与匹配。中文可以只输入两三个字。";
  }
}

/**
 * @param {HTMLElement} host
 * @param {{query:string,total:number,took:number,hits:any[]}} res
 * @param {string} q
 * @param {() => void} clearCategory
 */
function renderHits(host, res, q, clearCategory) {
  host.textContent = "";

  if (!res.hits || res.hits.length === 0) {
    const box = el("div", "empty");
    box.append(el("div", "empty-title", `没有找到「${q}」`));
    box.append(el("div", "empty-desc", "试试更短的关键词、换个说法，或者不限分类再看一遍。"));
    const btn = el("button", "btn-ghost", "不限分类再看一遍");
    btn.type = "button";
    btn.addEventListener("click", clearCategory);
    box.append(btn);
    host.append(box);
    return;
  }

  const meta = el("div", "search-meta", `命中 ${res.total} 条 · ${res.took} ms`);
  host.append(meta);

  const list = el("ul", "hits");
  for (const hit of res.hits) {
    const li = el("li", "hit");
    const a = /** @type {HTMLAnchorElement} */ (el("a", "hit-link"));
    a.href = docHref(hit.path);

    const head = el("div", "hit-head");
    head.append(el("span", "hit-name", hit.name));
    head.append(el("span", "hit-cat", hit.category));
    head.append(el("span", "hit-count", `${hit.count} 处`));
    a.append(head);

    const snippet = el("p", "hit-snippet");
    appendSnippet(snippet, hit.snippet);
    a.append(snippet);

    li.append(a);
    list.append(li);
  }
  host.append(list);
}

/**
 * 把服务端片段塞进 DOM：只认 <em>/</em>，其余按转义文本处理。
 * @param {HTMLElement} host
 * @param {string} snippet
 */
function appendSnippet(host, snippet) {
  const parts = String(snippet || "").split(/(<em>|<\/em>)/);
  /** @type {HTMLElement|null} */
  let em = null;
  for (const part of parts) {
    if (part === "<em>") {
      em = document.createElement("em");
      continue;
    }
    if (part === "</em>") {
      if (em) {
        host.append(em);
        em = null;
      }
      continue;
    }
    if (!part) continue;
    const span = document.createElement("span");
    span.textContent = decodeEntities(part);
    if (em) em.append(span);
    else host.append(span);
  }
  if (em) host.append(em);
}

/**
 * 服务端只转义这五个实体（search.htmlEscape），所以这里只解这五个。
 * @param {string} s
 */
function decodeEntities(s) {
  return s
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&amp;/g, "&");
}

/** @param {string} q */
function pushHistory(q) {
  const prev = /** @type {string[]} */ (getJSON(HISTORY_KEY, []));
  const list = Array.isArray(prev) ? prev.filter((x) => x !== q) : [];
  list.unshift(q);
  setJSON(HISTORY_KEY, list.slice(0, HISTORY_MAX));
}

/**
 * @param {{signal:AbortSignal}} ctx
 * @returns {Promise<string[]>}
 */
function loadCategories(ctx) {
  if (!catalogPromise) {
    catalogPromise = api.catalog(ctx.signal).catch((err) => {
      catalogPromise = undefined;
      throw err;
    });
  }
  return catalogPromise.then((cat) => (cat.categories || []).map((c) => c.name));
}

/**
 * @param {string} tag
 * @param {string} [cls]
 * @param {string} [text]
 */
function el(tag, cls, text) {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}
