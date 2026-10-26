// @ts-check
/**
 * localStorage 封装（设计规格 §5.4）：所有键统一前缀 readtool.
 * 读失败一律返回兜底值，绝不让隐私模式把整页打挂。
 */

const PREFIX = "readtool.";

/**
 * @template T
 * @param {string} key
 * @param {T} fallback
 * @returns {T}
 */
export function getJSON(key, fallback) {
  try {
    const raw = localStorage.getItem(PREFIX + key);
    if (raw === null) return fallback;
    return /** @type {T} */ (JSON.parse(raw));
  } catch (e) {
    return fallback;
  }
}

/**
 * @param {string} key
 * @param {unknown} value
 */
export function setJSON(key, value) {
  try {
    localStorage.setItem(PREFIX + key, JSON.stringify(value));
  } catch (e) {
    /* 配额满或隐私模式：忽略，不影响阅读 */
  }
}

/** @param {string} key @param {string} fallback */
export function getString(key, fallback) {
  try {
    const v = localStorage.getItem(PREFIX + key);
    return v === null ? fallback : v;
  } catch (e) {
    return fallback;
  }
}

/** @param {string} key @param {string} value */
export function setString(key, value) {
  try {
    localStorage.setItem(PREFIX + key, value);
  } catch (e) {
    /* 忽略 */
  }
}

/** @param {string} key */
export function remove(key) {
  try {
    localStorage.removeItem(PREFIX + key);
  } catch (e) {
    /* 忽略 */
  }
}

/**
 * 一次删多个键（排版设置的「恢复默认」用）。
 * @param {string[]} keys
 */
export function removeKeys(keys) {
  for (const k of keys) remove(k);
}
