// @ts-check
/**
 * 宽表三档降级（FR-04 / ADR-005）。
 *
 * 判定依据是「容器实测宽度 ÷ 列数」，不是 window.innerWidth，
 * 也不是@media 断点 —— 分屏、台前调度、字体放大都会改变它。
 *
 *   cell ≥ minCellPx        → table   普通表格
 *   cardViewPx ≤ cell < min → scroll  横滑 + 首列冻结
 *   cell < cardViewPx       → card    每行一张卡片
 */

/**
 * @param {HTMLElement} root 渲染出来的正文容器
 * @param {{minCellPx?:number, cardViewPx?:number}} [opts]
 * @returns {() => void} 清理函数
 */
export function enhanceTables(root, opts = {}) {
  const minCell = opts.minCellPx != null ? opts.minCellPx : numFromDataset("minCellPx", 130);
  const cardView = opts.cardViewPx != null ? opts.cardViewPx : numFromDataset("cardViewPx", 90);

  /** @type {HTMLElement[]} */
  const wraps = Array.from(root.querySelectorAll(".table-wrap"));

  /** @type {Map<HTMLElement, number>} */
  const timers = new Map();

  for (const wrap of wraps) {
    labelCells(wrap);
    applyMode(wrap, minCell, cardView);
  }

  /** @type {ResizeObserver|null} */
  let ro = null;
  if (typeof ResizeObserver !== "undefined" && wraps.length > 0) {
    ro = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const wrap = /** @type {HTMLElement} */ (entry.target);
        // 150ms 防抖：拖动窗口时不要每个像素都重算（ADR-012）
        const prev = timers.get(wrap);
        if (prev !== undefined) clearTimeout(prev);
        timers.set(
          wrap,
          window.setTimeout(() => {
            timers.delete(wrap);
            applyMode(wrap, minCell, cardView);
          }, 150),
        );
      }
    });
    // 只观察 .table-wrap：观察内部 <table> 会因为自己改宽度而反复触发
    for (const wrap of wraps) ro.observe(wrap);
  }

  return () => {
    if (ro) ro.disconnect();
    for (const t of timers.values()) clearTimeout(t);
    timers.clear();
  };
}

/**
 * @param {HTMLElement} wrap
 * @param {number} minCell
 * @param {number} cardView
 */
function applyMode(wrap, minCell, cardView) {
  const cols = Number(wrap.dataset.cols || "0");
  const width = wrap.clientWidth;
  if (!(cols > 0) || width <= 0) return;

  // 用四舍五入后的整数判定：避免 89.6 与 90 这种边界抖动导致同一张表在
  // 相差 1px 的两个视口里换模式（画面上也是显示这个整数，判定与显示一致）
  const cell = Math.round(width / cols);
  const mode = cell >= minCell ? "table" : cell >= cardView ? "scroll" : "card";
  if (wrap.dataset.mode !== mode) {
    wrap.dataset.mode = mode;
    wrap.dataset.cellPx = String(cell);
  }
}

/**
 * 把表头文字写到每个单元格的 data-label 上，卡片视图靠它显示字段名。
 * @param {HTMLElement} wrap
 */
function labelCells(wrap) {
  const table = wrap.querySelector("table");
  if (!table) return;
  const heads = Array.from(table.querySelectorAll("thead th")).map((th) =>
    (th.textContent || "").trim(),
  );
  if (heads.length === 0) return;

  for (const tr of Array.from(table.querySelectorAll("tbody tr"))) {
    const cells = Array.from(tr.children);
    cells.forEach((cell, i) => {
      const el = /** @type {HTMLElement} */ (cell);
      if (heads[i]) el.dataset.label = heads[i];
    });
  }
}

/**
 * @param {string} name
 * @param {number} fallback
 */
function numFromDataset(name, fallback) {
  const v = Number(document.documentElement.dataset[name]);
  return Number.isFinite(v) && v > 0 ? v : fallback;
}
