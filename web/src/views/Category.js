// @ts-check
/**
 * 分类页：该分类下的条目列表（FR-03）。
 */

import { api } from "/src/api.js?v=__RT_ASSET_V__";
import { docHref } from "/src/router.js?v=__RT_ASSET_V__";
import { listRecent } from "/src/lib/progress.js?v=__RT_ASSET_V__";
import { setTopbar } from "/src/components/AppShell.js?v=__RT_ASSET_V__";
import { sectionTitle, errorBox, describe, isAbort } from "/src/lib/dom.js?v=__RT_ASSET_V__";

/**
 * @param {HTMLElement} view
 * @param {{category:string}} params
 * @param {{signal:AbortSignal}} ctx
 */
export async function renderCategory(view, params, ctx) {
  const name = params.category;
  setTopbar({ title: name, backTo: "/" });
  view.innerHTML = `<div class="loading">载入中…</div>`;

  /** @type {any} */
  let catalog;
  try {
    catalog = await api.catalog(ctx.signal);
  } catch (err) {
    if (isAbort(err)) return;
    view.innerHTML = "";
    view.append(errorBox("无法载入分类", describe(err)));
    return;
  }
  if (ctx.signal.aborted) return;

  const cat = (catalog.categories || []).find((c) => c.name === name);
  view.textContent = "";
  const pad = document.createElement("div");
  pad.className = "pad";
  view.append(pad);

  if (!cat) {
    pad.append(errorBox("分类不存在", `没有找到「${name}」这个分类。可能知识库已经调整过。`));
    return;
  }

  const h1 = document.createElement("h1");
  h1.className = "home-title";
  h1.textContent = cat.name;
  pad.append(h1);

  if (cat.desc) {
    const desc = document.createElement("p");
    desc.className = "muted";
    desc.textContent = cat.desc;
    pad.append(desc);
  }

  pad.append(sectionTitle("条目", String(cat.count)));

  const list = document.createElement("ul");
  list.className = "entry-list";
  /** @type {Map<string, {ratio:number}>} */
  const recentMap = new Map(listRecent(12).map((r) => [r.path, r]));

  for (const entry of cat.entries || []) {
    const li = document.createElement("li");
    const a = document.createElement("a");
    a.className = "entry-row";
    a.href = docHref(entry.path);

    const title = document.createElement("span");
    title.className = "entry-name";
    title.textContent = entry.name;

    a.append(title);

    if (entry.summary) {
      const sm = document.createElement("span");
      sm.className = "entry-summary";
      sm.textContent = entry.summary;
      a.append(sm);
    }

    const meta = document.createElement("span");
    meta.className = "entry-meta";
    const size = formatSize(entry.size);
    meta.textContent = size;
    const r = recentMap.get(entry.path);
    if (r) meta.textContent += ` · 已读 ${Math.round(r.ratio * 100)}%`;
    a.append(meta);

    li.append(a);
    list.append(li);
  }
  pad.append(list);
}

/** @param {number} bytes */
function formatSize(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
