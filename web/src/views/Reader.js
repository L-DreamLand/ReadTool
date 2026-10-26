// @ts-check
/**
 * 阅读页（FR-05/FR-06/FR-19）。
 * 正文滚动容器是 #view —— 抽屉开合不会改变滚动位置（AC-19-4）。
 */

import { api, ApiError } from "/src/api.js?v=__RT_ASSET_V__";
import { catHref, docHref } from "/src/router.js?v=__RT_ASSET_V__";
import {
  setTopbar,
  readingActions,
  drawer,
  setBookmarkHandler,
  mountBookmarkAction,
  setBookmarkActive,
} from "/src/components/AppShell.js?v=__RT_ASSET_V__";
import { enhanceTables } from "/src/components/TableWrap.js?v=__RT_ASSET_V__";
import {
  bindProgressAutosave,
  getProgress,
  fetchServerProgress,
} from "/src/lib/progress.js?v=__RT_ASSET_V__";
import { errorBox, describe, isAbort } from "/src/lib/dom.js?v=__RT_ASSET_V__";

/**
 * @typedef {{id:string, text:string, level:number}} TocItem
 * @typedef {{path:string, name:string}} NavItem
 * @typedef {{path:string, name:string, title:string, category:string, html:string,
 *   toc:TocItem[], prev:NavItem|null, next:NavItem|null, wordCount:number}} Doc
 */

/**
 * @param {HTMLElement} view
 * @param {{path:string}} params
 * @param {{signal:AbortSignal}} ctx
 */
export async function renderReader(view, params, ctx) {
  const path = params.path;
  setTopbar({ title: "载入中…", backTo: "/" });
  view.innerHTML = `<div class="loading">载入中…</div>`;

  /** @type {Doc} */
  let doc;
  /** @type {any} */
  let catalog = null;
  /** @type {any} */
  let bookmarks = null;
  try {
    const [d, c, b] = await Promise.all([
      api.doc(path, ctx.signal),
      // 目录抽屉的第二个 tab 需要全量条目；失败不影响正文
      api.catalog(ctx.signal).catch(() => null),
      // 书签拿不到只影响 ☆ 的初始状态，不该挡住阅读
      api.bookmarks(ctx.signal).catch(() => null),
    ]);
    doc = /** @type {Doc} */ (d);
    catalog = c;
    bookmarks = b;
  } catch (err) {
    if (isAbort(err)) return;
    // 篇目在磁盘上没了：交给 main.js 渲染专用页（带「重新扫描目录」按钮），
    // 这里不能自己吞掉，否则用户只看到「打不开」却没有出路。
    if (err instanceof ApiError && err.code === "kb.path_not_found") throw err;
    view.innerHTML = "";
    const box = errorBox(
      "这一篇打不开",
      err instanceof Error && err.message ? err.message : describe(err),
    );
    view.append(box);
    setTopbar({ title: "打不开", backTo: "/" });
    return;
  }
  if (ctx.signal.aborted) return;

  const backTo = doc.category ? catHref(doc.category) : "/";
  setTopbar({
    title: doc.name || doc.title || path,
    backTo,
    toc: doc.toc.length > 0,
    actions: readingActions(),
    // 顶栏已经 ☆ ⟳ A ◐ 四个，再加 🔍 会挤掉标题；搜索入口放进目录抽屉
    search: false,
  });

  // ---------- 正文 ----------
  view.textContent = "";
  const wrap = document.createElement("div");
  wrap.className = "reader-wrap";

  const article = document.createElement("article");
  article.className = "markdown";
  // 内容来自自己的知识库，由服务端 goldmark 渲染（无用户输入），这里直接注入
  article.innerHTML = doc.html;
  wrap.append(article);
  view.append(wrap);

  // 代码块复制（FR-16）：局域网是 http，非安全上下文拿不到 navigator.clipboard，
  // 所以必须准备 execCommand 兜底，否则平板上一按就没反应。
  const onCopyClick = (ev) => {
    const btn = /** @type {HTMLElement|null} */ (
      /** @type {HTMLElement} */ (ev.target).closest(".code-copy")
    );
    if (!btn) return;
    const block = btn.closest(".code-block");
    const code = block ? block.querySelector("code") : null;
    if (!code) return;
    const text = code.textContent || "";
    copyText(text).then((ok) => {
      btn.textContent = ok ? "已复制" : "复制失败";
      btn.classList.toggle("is-done", ok);
      window.setTimeout(() => {
        btn.textContent = "复制";
        btn.classList.remove("is-done");
      }, 1600);
    });
  };
  article.addEventListener("click", onCopyClick);

  const disposeTables = enhanceTables(article, {
    minCellPx: numData("minCellPx", 130),
    cardViewPx: numData("cardViewPx", 90),
  });

  // ---------- 页脚：字数、原文、上下篇 ----------
  const foot = document.createElement("div");
  foot.className = "reader-foot";
  const words = document.createElement("span");
  words.textContent = `${doc.wordCount} 字`;
  const catLink = document.createElement("a");
  catLink.href = backTo;
  catLink.textContent = doc.category ? `分类：${doc.category}` : "返回首页";
  foot.append(words, catLink);

  // ---------- 原文操作：链接 / 下载 / 分享（FR-17） ----------
  // 局域网是 http://，Web Share API 要安全上下文才存在，
  // 所以「分享」在真机上一半情况会退化成「复制链接」——这正是期望行为。
  const shareURL = new URL(docHref(doc.path), location.origin).href;
  const actions = document.createElement("div");
  actions.className = "reader-actions";

  const sourceLink = document.createElement("a");
  sourceLink.className = "reader-action";
  sourceLink.href = api.rawURL(doc.path);
  sourceLink.textContent = "查看源码";
  sourceLink.target = "_blank";
  sourceLink.rel = "noopener";

  const copyLink = document.createElement("button");
  copyLink.type = "button";
  copyLink.className = "reader-action";
  copyLink.textContent = "复制链接";
  copyLink.addEventListener("click", () => {
    void copyText(shareURL).then((ok) => flashNote(wrap, ok ? "链接已复制" : "复制失败"));
  });

  const downloadLink = document.createElement("a");
  downloadLink.className = "reader-action";
  downloadLink.href = api.rawURL(doc.path, { download: true });
  downloadLink.textContent = "下载 .md";
  downloadLink.setAttribute("download", "");

  const shareBtn = document.createElement("button");
  shareBtn.type = "button";
  shareBtn.className = "reader-action";
  shareBtn.textContent = "分享";
  shareBtn.addEventListener("click", () => {
    if (typeof navigator.share === "function") {
      navigator.share({ title: doc.name || doc.title || "ReadTool", url: shareURL }).catch(() => {
        /* 用户取消，不算错误 */
      });
      return;
    }
    void copyText(shareURL).then((ok) =>
      flashNote(wrap, ok ? "已复制链接，粘贴即可分享" : "复制失败"),
    );
  });

  actions.append(sourceLink, copyLink, downloadLink, shareBtn);
  wrap.append(foot);
  // 单独一行：页脚是 0.72em 的小字，操作按钮要 44px 以上的触摸目标，不能挤在里面
  wrap.append(actions);

  const nav = document.createElement("div");
  nav.className = "reader-nav";
  nav.append(navButton(doc.prev, "‹ 上一篇", "start"), navButton(doc.next, "下一篇 ›", "end"));
  wrap.append(nav);

  // ---------- 抽屉内容 ----------
  /** @type {Array<{key:string,label:string,items:any[],groups?:boolean}>} */
  const tabs = [
    {
      key: "toc",
      label: "本篇目录",
      items: doc.toc.map((t) => ({ id: t.id, label: t.text, level: t.level })),
    },
  ];
  if (catalog) {
    tabs.push({
      key: "all",
      label: "全部条目",
      groups: true,
      // 第一行是「搜索全文」工具行（kind=tool），下面才是分类分组
      items: [{ id: "/search", label: "🔍 搜索全文", kind: "tool" }, ...catalogItems(catalog, doc.path)],
    });
  }
  if (tabs[0].items.length > 0 || tabs.length > 1) {
    drawer.setTabs(tabs, "toc");
  }

  // ---------- 书签（FR-11）：☆ 收藏"当前所在章节"，在文章开头则收藏整篇 ----------
  /** @type {Map<string,string>} anchorId（空串 = 条目级）→ 书签 id */
  const marks = new Map();
  if (bookmarks && Array.isArray(bookmarks.items)) {
    for (const b of bookmarks.items) {
      if (b && b.path === doc.path) marks.set(b.anchorId || "", b.id);
    }
  }
  /** @type {Record<string,string>} */
  const headingText = {};
  for (const t of doc.toc) headingText[t.id] = t.text;

  let activeAnchor = "";
  const refreshStar = () => setBookmarkActive(marks.has(activeAnchor));

  const toggleBookmark = async () => {
    const anchorId = activeAnchor;
    const existing = marks.get(anchorId);
    try {
      if (existing) {
        await api.delBookmark(existing);
        marks.delete(anchorId);
        flashNote(wrap, "已取消书签");
      } else {
        const r = await api.addBookmark({
          path: doc.path,
          anchorId,
          anchorText: anchorId ? headingText[anchorId] || "" : "",
        });
        marks.set(anchorId, r.id);
        flashNote(wrap, anchorId ? `已收藏：${headingText[anchorId] || "本节"}` : "已收藏整篇");
      }
    } catch (err) {
      const limit = err instanceof ApiError && err.code === "state.limit_exceeded";
      flashNote(wrap, limit ? "书签满了（500 条），先在书签页删几条" : "书签操作失败，稍后再试");
      return;
    }
    refreshStar();
  };

  setBookmarkHandler(() => void toggleBookmark());
  mountBookmarkAction(true);
  refreshStar();

  // ---------- 进度：本地先恢复，再用服务端校准（跨设备，FR-10） ----------
  const local = getProgress(doc.path);
  let restoreRatio = local && local.ratio > 0.02 && local.ratio < 0.999 ? local.ratio : 0;
  let resumeHint = null;

  const server = await fetchServerProgress(doc.path, ctx.signal);
  if (server && server.scrollPct > 0.02 && server.scrollPct < 0.999) {
    const localAt = local ? local.at : 0;
    const drift = Math.abs(server.scrollPct - (local ? local.ratio : 0));
    // 服务端的记录比本机新、且位置差得明显，才覆盖本地：否则一开页就跳走很烦
    if (server.updatedAt > localAt && drift > 0.02) {
      restoreRatio = server.scrollPct;
      resumeHint = server;
    }
  }
  if (ctx.signal.aborted) return;

  requestAnimationFrame(() => {
    const max = view.scrollHeight - view.clientHeight;
    if (restoreRatio > 0 && max > 0) view.scrollTop = Math.round(restoreRatio * max);
    // 首屏恢复之后再挂监听，避免把"跳转"当成一次滚动写回去
    bindScrollSpy(view, article, doc.toc, (id) => {
      activeAnchor = id;
      refreshStar();
    });
    if (resumeHint) showResumeNote(wrap, resumeHint);
  });

  const stopProgress = bindProgressAutosave(
    view,
    doc.path,
    () => ({ name: doc.name, category: doc.category }),
    () => activeAnchor,
  );

  // 路由切换时清理：由 main 通过返回的清理函数调用
  return () => {
    article.removeEventListener("click", onCopyClick);
    disposeTables();
    stopProgress();
  };
}

/**
 * 页面底部闪一条提示（保存成功/失败都用它，避免弹窗）。
 * @param {HTMLElement} host
 * @param {string} text
 */
function flashNote(host, text) {
  const el = document.createElement("div");
  el.className = "note-flash";
  el.textContent = text;
  host.append(el);
  window.setTimeout(() => el.remove(), 2200);
}

/**
 * 从别的设备续读：顶部给一条可关闭的提示，说明位置是谁给的。
 * @param {HTMLElement} host
 * @param {{scrollPct:number, device?:string}} server
 */
function showResumeNote(host, server) {
  const el = document.createElement("div");
  el.className = "resume-note";
  const pct = Math.round(server.scrollPct * 100);
  el.textContent = `已恢复到上次阅读的位置（${pct}%${server.device ? " · " + server.device : ""}）`;
  const close = document.createElement("button");
  close.type = "button";
  close.className = "resume-note-close";
  close.textContent = "×";
  close.setAttribute("aria-label", "关闭提示");
  close.addEventListener("click", () => el.remove());
  el.append(close);
  host.prepend(el);
}

/**
 * 监听滚动，高亮当前标题（只更新抽屉里的 active 状态）。
 * @param {HTMLElement} view
 * @param {HTMLElement} article
 * @param {TocItem[]} toc
 * @param {(id:string) => void} [onActive]
 */
function bindScrollSpy(view, article, toc, onActive) {
  /** @type {HTMLElement[]} */
  const headings = [];
  for (const item of toc) {
    const el = article.querySelector(`#${cssEscape(item.id)}`);
    if (el instanceof HTMLElement) headings.push(el);
  }
  if (headings.length === 0) return;

  let timer;
  view.addEventListener(
    "scroll",
    () => {
      if (timer !== undefined) return;
      timer = window.setTimeout(() => {
        timer = undefined;
        const top = view.getBoundingClientRect().top + 24;
        let current = headings[0];
        for (const h of headings) {
          if (h.getBoundingClientRect().top <= top) current = h;
          else break;
        }
        if (current && current.id) {
          drawer.markActive(current.id);
          if (onActive) onActive(current.id);
        }
      }, 200);
    },
    { passive: true },
  );
}

/**
 * @param {NavItem|null} item
 * @param {string} label
 * @param {string} align
 */
function navButton(item, label, align) {
  const a = document.createElement("a");
  a.className = "btn";
  a.style.justifyContent = align;
  if (item) {
    a.href = docHref(item.path);
    a.textContent = label;
    a.title = item.name;
  } else {
    a.href = "#";
    a.textContent = "—";
    a.setAttribute("aria-disabled", "true");
    a.addEventListener("click", (e) => e.preventDefault());
    a.style.visibility = "hidden";
  }
  return a;
}

/**
 * 全部条目：按分类分组，组名用 level=1 标记。
 * @param {any} catalog
 * @param {string} currentPath
 */
function catalogItems(catalog, currentPath) {
  /** @type {Array<{id:string,label:string,level?:number,current?:boolean}>} */
  const items = [];
  for (const cat of catalog.categories || []) {
    items.push({ id: "", label: `${cat.name}（${cat.count}）`, level: 1 });
    for (const entry of cat.entries || []) {
      items.push({
        id: docHref(entry.path),
        label: entry.name,
        current: entry.path === currentPath,
      });
    }
  }
  return items;
}

/** @param {string} id */
function cssEscape(id) {
  if (typeof CSS !== "undefined" && typeof CSS.escape === "function") return CSS.escape(id);
  return id.replace(/(["\\#.:>+~*\[\]()])/g, "\\$1");
}

/** @param {string} name @param {number} fallback */
function numData(name, fallback) {
  const v = Number(document.documentElement.dataset[name]);
  return Number.isFinite(v) && v > 0 ? v : fallback;
}

/**
 * 复制文本到剪贴板。
 * 安全上下文（https 或 localhost）优先用 clipboard API；局域网 http 下它不可用，
 * 退回 textarea + execCommand("copy")。
 * @param {string} text
 * @returns {Promise<boolean>}
 */
function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    return navigator.clipboard.writeText(text).then(
      () => true,
      () => legacyCopy(text),
    );
  }
  return Promise.resolve(legacyCopy(text));
}

/** @param {string} text @returns {boolean} */
function legacyCopy(text) {
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.style.position = "fixed";
  ta.style.top = "-1000px";
  ta.style.opacity = "0";
  document.body.append(ta);
  ta.select();
  ta.setSelectionRange(0, ta.value.length);
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch (e) {
    ok = false;
  }
  ta.remove();
  return ok;
}
