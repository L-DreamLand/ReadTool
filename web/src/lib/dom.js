// @ts-check
/**
 * DOM 小工具（视图共用）。
 */

/** @param {unknown} err */
export function describe(err) {
  if (err instanceof Error) return err.message;
  return String(err);
}

/** @param {unknown} err */
export function isAbort(err) {
  return err instanceof DOMException && err.name === "AbortError";
}

/**
 * @param {string} text
 * @param {string} [count]
 */
export function sectionTitle(text, count) {
  const el = document.createElement("div");
  el.className = "section-title";
  el.textContent = text;
  if (count) {
    const c = document.createElement("span");
    c.className = "count";
    c.textContent = count;
    el.append(c);
  }
  return el;
}

/**
 * @param {string} title
 * @param {string} desc
 * @param {{label:string, onClick:()=>void}[]} [actions] 可选的动作按钮（如「重新扫描」）
 */
export function errorBox(title, desc, actions) {
  const box = document.createElement("div");
  box.className = "error-box";
  const h = document.createElement("h2");
  h.textContent = title;
  const p = document.createElement("p");
  p.textContent = desc;
  box.append(h, p);
  if (actions && actions.length > 0) {
    const row = document.createElement("div");
    row.className = "error-actions";
    for (const a of actions) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "btn-ghost";
      btn.textContent = a.label;
      btn.addEventListener("click", a.onClick);
      row.append(btn);
    }
    box.append(row);
  }
  return box;
}

/** @param {string} s */
export function esc(s) {
  return s.replace(/[&<>"']/g, (ch) => {
    switch (ch) {
      case "&":
        return "&amp;";
      case "<":
        return "&lt;";
      case ">":
        return "&gt;";
      case '"':
        return "&quot;";
      default:
        return "&#39;";
    }
  });
}
