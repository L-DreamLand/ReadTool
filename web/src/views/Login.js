// @ts-check
/**
 * 登录 / 首次设置访问口令（FR-09）。
 * 首次启动时库里没有口令哈希，页面引导用户设置；之后每次只需输入。
 */

import { api } from "/src/api.js?v=__RT_ASSET_V__";
import { applyTheme } from "/src/components/Typography.js?v=__RT_ASSET_V__";

/**
 * @param {HTMLElement} view
 * @param {{needSetup:boolean, message?:string}} opts
 */
export function renderLogin(view, opts) {
  applyTheme(localStorage.getItem("readtool.theme") || "auto");

  view.textContent = "";
  const wrap = document.createElement("div");
  wrap.className = "login-wrap";
  view.append(wrap);

  const h1 = document.createElement("h1");
  h1.textContent = opts.needSetup ? "设置访问口令" : "输入访问口令";
  wrap.append(h1);

  const hint = document.createElement("p");
  hint.className = "hint";
  hint.textContent = opts.needSetup
    ? "第一次使用，请先给知识库设一个口令。设好后在平板上输一次，之后会自动记住。"
    : opts.message || "口令只在本机保存，用于挡住同一网络里的其他设备。";
  wrap.append(hint);

  const form = document.createElement("form");
  form.autocomplete = "on";

  const field = document.createElement("label");
  field.className = "field";
  const label = document.createElement("span");
  label.textContent = "口令";
  const input = document.createElement("input");
  input.type = "password";
  input.name = "password";
  input.required = true;
  input.autocomplete = opts.needSetup ? "new-password" : "current-password";
  input.placeholder = opts.needSetup ? "至少 8 位" : "";
  field.append(label, input);
  form.append(field);

  /** @type {HTMLInputElement|null} */
  let confirm = null;
  if (opts.needSetup) {
    const cf = document.createElement("label");
    cf.className = "field";
    const cl = document.createElement("span");
    cl.textContent = "再输一次";
    confirm = document.createElement("input");
    confirm.type = "password";
    confirm.name = "confirm";
    confirm.required = true;
    confirm.autocomplete = "new-password";
    cf.append(cl, confirm);
    form.append(cf);
  }

  const submit = document.createElement("button");
  submit.type = "submit";
  submit.className = "btn btn-primary btn-block";
  submit.textContent = opts.needSetup ? "设置并进入" : "进入";
  form.append(submit);

  const msg = document.createElement("p");
  msg.className = "form-msg";
  if (opts.message) msg.textContent = opts.message;
  form.append(msg);

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    msg.classList.remove("ok");
    msg.textContent = "";

    const pw = input.value;
    if (pw.length < 8) {
      msg.textContent = "口令至少 8 位。";
      return;
    }
    if (confirm && confirm.value !== pw) {
      msg.textContent = "两次输入不一致。";
      return;
    }

    submit.disabled = true;
    submit.textContent = opts.needSetup ? "设置中…" : "验证中…";
    try {
      await api.login(pw);
      msg.classList.add("ok");
      msg.textContent = "成功，正在进入…";
      // 重新加载：boot config 里的 authenticated/needSetup 由服务端重新计算
      location.assign("/");
    } catch (err) {
      submit.disabled = false;
      submit.textContent = opts.needSetup ? "设置并进入" : "进入";
      msg.textContent = loginError(err);
      input.select();
    }
  });

  wrap.append(form);
  input.focus();
}

/** @param {unknown} err */
function loginError(err) {
  if (err && typeof err === "object" && "code" in err) {
    const code = /** @type {any} */ (err).code;
    if (code === "auth.locked") return "尝试次数太多，请稍后再试。";
    if (code === "auth.bad_password") return "口令不对。";
    if (code === "bad_request") return "口令太短或格式不对。";
  }
  return err instanceof Error ? err.message : String(err);
}
