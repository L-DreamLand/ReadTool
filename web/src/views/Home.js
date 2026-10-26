// @ts-check
/**
 * 首页：分类网格 + 根目录文档 + 最近阅读（FR-02）。
 */

import { api } from "/src/api.js?v=__RT_ASSET_V__";
import { catHref, docHref } from "/src/router.js?v=__RT_ASSET_V__";
import { listRecent } from "/src/lib/progress.js?v=__RT_ASSET_V__";
import { setTopbar } from "/src/components/AppShell.js?v=__RT_ASSET_V__";
import { sectionTitle, errorBox, describe, isAbort } from "/src/lib/dom.js?v=__RT_ASSET_V__";

/**
 * @param {HTMLElement} view
 * @param {{signal:AbortSignal}} ctx
 */
export async function renderHome(view, ctx) {
  setTopbar({ title: "ReadTool" });
  view.innerHTML = `<div class="loading">载入中…</div>`;

  /** @type {any} */
  let catalog;
  try {
    catalog = await api.catalog(ctx.signal);
  } catch (err) {
    if (isAbort(err)) return;
    view.innerHTML = "";
    view.append(errorBox("无法载入知识库", describe(err)));
    return;
  }
  if (ctx.signal.aborted) return;

  view.textContent = "";
  const pad = document.createElement("div");
  pad.className = "pad";
  view.append(pad);

  const h1 = document.createElement("h1");
  h1.className = "home-title";
  h1.textContent = "知识库";
  pad.append(h1);

  const meta = document.createElement("p");
  meta.className = "hint";
  meta.textContent = `${catalog.total} 条 · ${catalog.categories.length} 个分类`;
  pad.append(meta);

  // 最近阅读（FR-12）：以服务端为准（跨设备）；服务端不可达时退回本机缓存
  /** @type {Array<{path:string,name:string,category:string,scrollPct:number}>} */
  let recent = [];
  let bookmarkCount = 0;
  try {
    const [r, bm] = await Promise.all([
      api.recent(5, ctx.signal),
      api.bookmarks(ctx.signal).catch(() => null),
    ]);
    recent = (r && r.items) || [];
    if (bm && Array.isArray(bm.items)) bookmarkCount = bm.items.length;
  } catch (err) {
    if (isAbort(err)) return;
    recent = listRecent(4).map((it) => ({
      path: it.path,
      name: it.name,
      category: it.category,
      scrollPct: it.ratio,
    }));
  }
  if (ctx.signal.aborted) return;

  // 快捷入口：书签 / 最近阅读（两者都是"跨设备的阅读痕迹"）
  const quick = document.createElement("div");
  quick.className = "quick-links";
  quick.append(
    quickLink("书签", bookmarkCount > 0 ? String(bookmarkCount) : "", "/bookmarks"),
    quickLink("最近阅读", recent.length > 0 ? String(recent.length) : "", "/recent"),
  );
  pad.append(quick);

  if (recent.length > 0) {
    pad.append(sectionTitle("最近阅读"));
    const list = document.createElement("ul");
    list.className = "entry-list";
    for (const item of recent) {
      const li = document.createElement("li");
      const a = document.createElement("a");
      a.className = "entry-row";
      a.href = docHref(item.path);
      const name = document.createElement("span");
      name.className = "entry-name";
      name.textContent = item.name || item.path;
      const sm = document.createElement("span");
      sm.className = "entry-summary";
      const pct = Math.round((item.scrollPct || 0) * 100);
      sm.textContent = item.category ? `${item.category} · 已读 ${pct}%` : `已读 ${pct}%`;
      a.append(name, sm);
      li.append(a);
      list.append(li);
    }
    pad.append(list);
  }

  // 分类
  pad.append(sectionTitle("分类", `${catalog.categories.length}`));
  const grid = document.createElement("div");
  grid.className = "cat-grid";
  for (const cat of catalog.categories) {
    const a = document.createElement("a");
    a.className = "cat-card";
    a.href = catHref(cat.name);

    const name = document.createElement("span");
    name.className = "cat-name";
    name.textContent = cat.name;

    const desc = document.createElement("span");
    desc.className = "cat-desc";
    desc.textContent = cat.desc || "";

    const count = document.createElement("span");
    count.className = "cat-meta";
    count.textContent = `${cat.count} 条`;

    a.append(name, desc, count);
    grid.append(a);
  }
  pad.append(grid);

  // 根目录文档（INDEX.md / AGENTS.md）
  const roots = Array.isArray(catalog.rootFiles) ? catalog.rootFiles : [];
  if (roots.length > 0) {
    pad.append(sectionTitle("根目录"));
    const list = document.createElement("ul");
    list.className = "entry-list";
    for (const file of roots) {
      const li = document.createElement("li");
      const a = document.createElement("a");
      a.className = "entry-row";
      a.href = docHref(file.path);
      const name = document.createElement("span");
      name.className = "entry-name";
      name.textContent = file.name;
      const sm = document.createElement("span");
      sm.className = "entry-summary";
      sm.textContent = file.kind === "index" ? "知识地图（总索引）" : file.kind === "agents" ? "运行规范" : "根目录文档";
      a.append(name, sm);
      li.append(a);
      list.append(li);
    }
    pad.append(list);
  }
}

/**
 * 首页的胶囊入口。
 * @param {string} label
 * @param {string} badge 空字符串表示不显示计数
 * @param {string} href
 */
function quickLink(label, badge, href) {
  const a = document.createElement("a");
  a.className = "quick-link";
  a.href = href;
  a.textContent = label;
  if (badge) {
    const b = document.createElement("span");
    b.className = "quick-badge";
    b.textContent = badge;
    a.append(b);
  }
  return a;
}
