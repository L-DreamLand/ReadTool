// @ts-check
/**
 * 最近阅读页（FR-12）：服务端按「最后写入时间」倒序给出，跨设备一致。
 * 与首页那块的区别：这里给到 20 条，并显示具体进度。
 */

import { api } from "/src/api.js?v=__RT_ASSET_V__";
import { docHref, navigate } from "/src/router.js?v=__RT_ASSET_V__";
import { setTopbar } from "/src/components/AppShell.js?v=__RT_ASSET_V__";
import { sectionTitle, errorBox, describe, isAbort } from "/src/lib/dom.js?v=__RT_ASSET_V__";

/**
 * @typedef {{path:string, name:string, category:string, scrollPct:number, updatedAt:number}} RecentItem
 */

/**
 * @param {HTMLElement} view
 * @param {{signal:AbortSignal}} ctx
 */
export async function renderRecent(view, ctx) {
  setTopbar({ title: "最近阅读", backTo: "/" });
  view.innerHTML = `<div class="loading">载入中…</div>`;

  /** @type {RecentItem[]} */
  let items;
  try {
    const res = await api.recent(20, ctx.signal);
    items = (res && res.items) || [];
  } catch (err) {
    if (isAbort(err)) return;
    view.innerHTML = "";
    view.append(errorBox("最近阅读读不出来", describe(err)));
    return;
  }
  if (ctx.signal.aborted) return;

  const pad = document.createElement("div");
  pad.className = "pad";
  view.textContent = "";
  view.append(pad);

  if (items.length === 0) {
    pad.append(
      errorBox(
        "还没有阅读记录",
        "读过的条目会出现在这里，并且跟着你的账号同步——换台设备也能接着读。",
        [{ label: "回到首页", onClick: () => navigate("/") }],
      ),
    );
    return;
  }

  pad.append(sectionTitle("最近阅读", String(items.length)));
  const list = document.createElement("ul");
  list.className = "entry-list";
  for (const it of items) {
    const li = document.createElement("li");
    const a = document.createElement("a");
    a.className = "entry-row";
    a.href = docHref(it.path);

    const name = document.createElement("span");
    name.className = "entry-name";
    name.textContent = it.name || it.path;

    const sm = document.createElement("span");
    sm.className = "entry-summary";
    const pct = Math.round((it.scrollPct || 0) * 100);
    const when = formatWhen(it.updatedAt);
    sm.textContent = `${it.category ? it.category + " · " : ""}已读 ${pct}%${when ? " · " + when : ""}`;

    a.append(name, sm);
    li.append(a);
    list.append(li);
  }
  pad.append(list);
}

/**
 * 时间给人看：今天只显示时分，更早显示日期。
 * @param {number} ms
 */
function formatWhen(ms) {
  if (!ms) return "";
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return "";
  const now = new Date();
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  if (sameDay) return `今天 ${hh}:${mm}`;
  return `${d.getMonth() + 1}月${d.getDate()}日`;
}
