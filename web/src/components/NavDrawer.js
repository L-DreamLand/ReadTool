// @ts-check
/**
 * 目录抽屉（FR-19）。
 * - 覆盖式：position:fixed，不挤压正文，开合不改变正文滚动位置（AC-19-4）
 * - 两个入口：顶栏「☰」+ 右下悬浮按钮；**不做边缘手势**（iOS 侧滑返回会抢）
 * - 两个 tab：「本篇目录」（默认）/「全部条目」
 */

/**
 * @typedef {{id:string, label:string, level?:number, current?:boolean, kind?:string}} DrawerItem
 *
 * `kind === "tool"` 的行不是章节锚点而是站内链接（id 就是 href），例如「搜索全文」。
 * @typedef {{key:string, label:string, items:DrawerItem[], groups?:boolean}} DrawerTab
 */

export class NavDrawer {
  /**
   * @param {HTMLElement} el 抽屉容器
   * @param {HTMLElement} mask 遮罩
   */
  constructor(el, mask) {
    this.el = el;
    this.mask = mask;
    this.body = /** @type {HTMLElement} */ (el.querySelector(".drawer-body"));
    this.tabsEl = /** @type {HTMLElement} */ (el.querySelector(".drawer-tabs"));
    /** @type {DrawerTab[]} */
    this.tabs = [];
    this.activeKey = "";
    this.open = false;
    /** @type {string|null} */
    this.currentID = null;

    this.mask.addEventListener("click", () => this.close());
    this.el.addEventListener("click", (e) => {
      const target = /** @type {HTMLElement} */ (e.target);
      const a = target.closest("a");
      if (a) {
        // 抽屉里的导航：先关再走，避免目标页出现时抽屉还挂着
        this.close();
      }
    });
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape" && this.open) this.close();
    });
  }

  /**
   * 设置内容并打开。
   * @param {DrawerTab[]} tabs
   * @param {string} [activeKey]
   */
  show(tabs, activeKey) {
    this.tabs = tabs;
    this.activeKey = activeKey || (tabs[0] ? tabs[0].key : "");
    this.renderTabs();
    this.renderBody();
    this.el.hidden = false;
    this.mask.hidden = false;
    // 先显示再加 class，保证 transition 生效
    requestAnimationFrame(() => {
      this.el.classList.add("open");
      this.mask.classList.add("show");
    });
    this.open = true;
    this.el.setAttribute("aria-hidden", "false");
  }

  /**
   * 预先装填内容但不打开（阅读页渲染完就调用，点「☰」时才滑出）。
   * @param {DrawerTab[]} tabs
   * @param {string} [activeKey]
   */
  setTabs(tabs, activeKey) {
    this.tabs = tabs;
    const keys = tabs.map((t) => t.key);
    this.activeKey = activeKey && keys.includes(activeKey) ? activeKey : keys[0] || "";
  }

  close() {
    if (!this.open) return;
    this.open = false;
    this.el.classList.remove("open");
    this.mask.classList.remove("show");
    this.el.setAttribute("aria-hidden", "true");
    const done = () => {
      if (this.open) return; // 期间又被打开
      this.el.hidden = true;
      this.mask.hidden = true;
    };
    this.el.addEventListener("transitionend", done, { once: true });
    // 兜底：某些环境不触发 transitionend
    window.setTimeout(() => {
      if (!this.open) done();
    }, 260);
  }

  toggle() {
    if (this.open) this.close();
    else if (this.tabs.length > 0) this.show(this.tabs, this.activeKey);
  }

  renderTabs() {
    this.tabsEl.textContent = "";
    if (this.tabs.length < 2) {
      this.tabsEl.hidden = true;
      return;
    }
    this.tabsEl.hidden = false;
    for (const tab of this.tabs) {
      const btn = document.createElement("button");
      btn.className = "drawer-tab";
      btn.type = "button";
      btn.textContent = tab.label;
      btn.setAttribute("aria-selected", String(tab.key === this.activeKey));
      btn.addEventListener("click", () => {
        this.activeKey = tab.key;
        this.renderTabs();
        this.renderBody();
        this.body.scrollTop = 0;
      });
      this.tabsEl.append(btn);
    }
  }

  renderBody() {
    this.body.textContent = "";
    const tab = this.tabs.find((t) => t.key === this.activeKey) || this.tabs[0];
    if (!tab) return;

    if (tab.groups) return renderGrouped(this.body, tab.items, this.currentID);

    const list = document.createElement("ul");
    list.className = "toc-list";
    for (const item of tab.items) {
      const li = document.createElement("li");
      const a = document.createElement("a");
      a.className = "toc-link" + (item.level === 3 ? " lv3" : "");
      a.href = "#" + item.id;
      a.textContent = item.label;
      a.dataset.id = item.id;
      if (item.id === this.currentID) a.classList.add("active");
      a.addEventListener("click", (e) => {
        e.preventDefault();
        this.close();
        this.scrollToHeading(item.id);
      });
      li.append(a);
      list.append(li);
    }
    this.body.append(list);
  }

  /** @param {string} id */
  scrollToHeading(id) {
    const target = document.getElementById(id);
    if (!target) return;
    const view = document.getElementById("view");
    if (!view) return;
    const top =
      target.getBoundingClientRect().top - view.getBoundingClientRect().top + view.scrollTop - 8;
    view.scrollTo({ top, behavior: "smooth" });
    this.currentID = id;
    this.markActive(id);
  }

  /** @param {string} id */
  markActive(id) {
    this.currentID = id;
    for (const a of Array.from(this.body.querySelectorAll(".toc-link"))) {
      const el = /** @type {HTMLElement} */ (a);
      el.classList.toggle("active", el.dataset.id === id);
    }
  }
}

/**
 * 「全部条目」= 分类分组 + 条目链接。
 * @param {HTMLElement} body
 * @param {DrawerItem[]} items
 * @param {string|null} currentID
 */
function renderGrouped(body, items, currentID) {
  const list = document.createElement("ul");
  list.className = "drawer-list";
  for (const item of items) {
    if (item.kind === "tool") {
      const li = document.createElement("li");
      const a = document.createElement("a");
      a.className = "drawer-link drawer-tool";
      a.href = item.id;
      a.textContent = item.label;
      li.append(a);
      list.append(li);
      continue;
    }
    if (item.level === 1) {
      const head = document.createElement("li");
      head.className = "drawer-group";
      head.textContent = item.label;
      list.append(head);
      continue;
    }
    const li = document.createElement("li");
    const a = document.createElement("a");
    a.className = "drawer-link" + (item.id === currentID ? " current" : "");
    a.href = item.id;
    a.textContent = item.label;
    li.append(a);
    list.append(li);
  }
  body.append(list);
}
