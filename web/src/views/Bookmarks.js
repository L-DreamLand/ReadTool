// @ts-check
/**
 * 书签页（FR-11）：列出全部书签，可跳转、可删除。
 * 书签是服务端的（跨设备一致），删完立刻从列表消失。
 */

import { api } from "/src/api.js?v=__RT_ASSET_V__";
import { docHref, navigate } from "/src/router.js?v=__RT_ASSET_V__";
import { setTopbar } from "/src/components/AppShell.js?v=__RT_ASSET_V__";
import { sectionTitle, errorBox, describe, isAbort } from "/src/lib/dom.js?v=__RT_ASSET_V__";

/**
 * @typedef {{id:string, path:string, name:string, category:string,
 *   anchorId?:string, anchorText?:string, createdAt:number, missing?:boolean}} Bookmark
 */

/**
 * @param {HTMLElement} view
 * @param {{signal:AbortSignal}} ctx
 */
export async function renderBookmarks(view, ctx) {
  setTopbar({ title: "书签", backTo: "/" });
  view.innerHTML = `<div class="loading">载入中…</div>`;

  /** @type {Bookmark[]} */
  let items;
  try {
    const res = await api.bookmarks(ctx.signal);
    items = (res && res.items) || [];
  } catch (err) {
    if (isAbort(err)) return;
    view.innerHTML = "";
    view.append(errorBox("书签读不出来", describe(err)));
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
        "还没有书签",
        "在阅读页点顶栏的 ☆ 就能收藏当前章节；书签会同步到你的所有设备。",
        [{ label: "回到首页", onClick: () => navigate("/") }],
      ),
    );
    return;
  }

  pad.append(sectionTitle("全部书签", String(items.length)));
  const list = document.createElement("ul");
  list.className = "entry-list";

  for (const b of items) {
    const li = document.createElement("li");
    li.className = "bm-item";

    const a = document.createElement("a");
    a.className = "entry-row";
    // 锚点级书签要带上 #id，打开后浏览器会自己滚到那一节
    a.href = b.missing ? "#" : docHref(b.path) + (b.anchorId ? `#${encodeURIComponent(b.anchorId)}` : "");

    const name = document.createElement("span");
    name.className = "entry-name";
    name.textContent = b.missing ? b.path : b.name || b.path;

    const sm = document.createElement("span");
    sm.className = "entry-summary";
    if (b.missing) {
      sm.textContent = "条目已不存在（文件被删或改名）";
      a.classList.add("is-missing");
    } else {
      const where = b.category || "";
      const anchor = b.anchorText ? ` · ${b.anchorText}` : "";
      sm.textContent = `${where}${anchor}`;
    }
    a.append(name, sm);

    if (b.missing) {
      // 死链不给跳，只留删除
      a.addEventListener("click", (e) => e.preventDefault());
    }

    const del = document.createElement("button");
    del.type = "button";
    del.className = "icon-btn bm-del";
    del.textContent = "×";
    del.title = "删除书签";
    del.setAttribute("aria-label", "删除书签");
    del.addEventListener("click", async (e) => {
      e.preventDefault();
      e.stopPropagation();
      del.disabled = true;
      try {
        await api.delBookmark(b.id);
        li.remove();
      } catch (err) {
        del.disabled = false;
        del.title = err instanceof Error && err.message ? err.message : "删除失败";
      }
    });

    li.append(a, del);
    list.append(li);
  }

  pad.append(list);

  // 分类入口留着，删完书签顺手能继续逛
  const back = document.createElement("p");
  back.className = "hint";
  const home = document.createElement("a");
  home.href = "/";
  home.textContent = "回到首页继续找";
  back.append(home);
  pad.append(back);
}
