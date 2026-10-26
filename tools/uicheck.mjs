// CDP 驱动的阅读页验收（仅开发期使用，不属于 web/ 产物）
// 用法：node tools/uicheck.mjs <baseURL> <cookieValue> [path]
// 依赖：Node 26 自带的 fetch 与 WebSocket，无第三方包。

const [
  ,
  ,
  baseURL,
  cookieValue,
  targetPath = "/doc/%E4%BA%BA%E5%B7%A5%E6%99%BA%E8%83%BD/%E6%8A%80%E8%83%BD.md",
  widthArg = "800",
] = process.argv;
const width = Number(widthArg);

const DEVTOOLS = "http://127.0.0.1:9222";

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function main() {
  const version = await (await fetch(`${DEVTOOLS}/json/version`)).json();
  const ws = new WebSocket(version.webSocketDebuggerUrl);
  await new Promise((res, rej) => {
    ws.onopen = res;
    ws.onerror = rej;
  });

  let id = 0;
  /** @type {Map<number, (v:any)=>void>} */
  const pending = new Map();
  /** @type {string[]} */
  const errors = [];

  ws.onmessage = (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.id && pending.has(msg.id)) {
      pending.get(msg.id)(msg);
      pending.delete(msg.id);
      return;
    }
    if (msg.method === "Runtime.exceptionThrown") {
      errors.push(
        "exception: " +
          (msg.params.exceptionDetails?.exception?.description ||
            msg.params.exceptionDetails?.text),
      );
    }
    if (msg.method === "Runtime.consoleAPICalled" && msg.params.type === "error") {
      errors.push(
        "console.error: " +
          msg.params.args.map((a) => a.value ?? a.description ?? "").join(" "),
      );
    }
  };

  const send = (method, params = {}, sessionId) =>
    new Promise((resolve) => {
      const msgId = ++id;
      pending.set(msgId, resolve);
      ws.send(JSON.stringify({ id: msgId, method, params, sessionId }));
    });

  const { result: target } = await send("Target.createTarget", { url: "about:blank" });
  const { result: attached } = await send("Target.attachToTarget", {
    targetId: target.targetId,
    flatten: true,
  });
  const s = attached.sessionId;

  await send("Runtime.enable", {}, s);
  await send("Page.enable", {}, s);
  await send("Network.enable", {}, s);
  // 浏览器是常驻的（同一 9222 实例），不关缓存会读到上一次的 app.css / 组件 JS
  await send("Network.setCacheDisabled", { cacheDisabled: true }, s);

  await send(
    "Emulation.setDeviceMetricsOverride",
    { width, height: 1280, deviceScaleFactor: 2, mobile: true },
    s,
  );

  const cookie = JSON.parse(cookieValue);
  await send(
    "Network.setCookie",
    {
      name: cookie.name,
      value: cookie.value,
      domain: "127.0.0.1",
      path: "/",
      httpOnly: true,
      sameSite: "Lax",
    },
    s,
  );

  const url = baseURL.replace(/\/+$/, "") + "/" + String(targetPath).replace(/^\/+/, "");
  await send("Page.navigate", { url }, s);
  await sleep(2200);

  const expr = `(() => {
    const q = (sel) => Array.from(document.querySelectorAll(sel));
    const modes = q('.table-wrap').map(w => ({
      cols: w.dataset.cols,
      mode: w.dataset.mode,
      cell: w.dataset.cellPx,
      w: Math.round(w.clientWidth),
      sw: w.scrollWidth,
      tw: Math.round(w.querySelector("table")?.getBoundingClientRect().width ?? 0),
      ws: getComputedStyle(w.querySelector("td") || w).whiteSpace,
    }));
    const article = document.querySelector('article.markdown');
    return JSON.stringify({
      title: document.getElementById('topbar-title')?.textContent,
      hasArticle: !!article,
      textLen: article ? article.textContent.length : 0,
      h2: q('article h2').length,
      tables: modes,
      tocBtn: !document.getElementById('btn-toc')?.hidden,
      fab: !document.getElementById('btn-drawer-fab')?.hidden,
      backBtn: !document.getElementById('btn-back')?.hidden,
      actions: q('#topbar-actions button').map(b => b.textContent),
      drawerItems: q('#drawer .toc-link').length,
      drawerTabs: q('#drawer .drawer-tab').map(b => b.textContent),
      prevNext: q('.reader-nav .btn').map(a => a.textContent.trim() + '|' + a.getAttribute('href')),
      brokenLinks: q('article a.link-broken').length,
      docLinks: q('article a[href^="/doc/"]').length,
      fontPx: getComputedStyle(article || document.body).fontSize,
      viewFontPx: view ? getComputedStyle(view).fontSize : "",
      pFontPx: (() => { const p = document.querySelector("article p, #view p"); return p ? getComputedStyle(p).fontSize : ""; })(),
      catCards: document.querySelectorAll(".cat-card").length,
      entryRows: document.querySelectorAll(".entry-row, .entry-item").length,
      chips: document.querySelectorAll(".chip, .cat-count").length,
      rootFiles: document.querySelectorAll(".root-file").length,
      scrollH: document.getElementById('view')?.scrollHeight,
      loginForm: q('.login-wrap form').length,
      docSW: document.documentElement.scrollWidth,
      bodySW: document.body.scrollWidth,
      viewSW: document.getElementById('view')?.scrollWidth,
      viewCW: document.getElementById('view')?.clientWidth,
      artW: article ? Math.round(article.getBoundingClientRect().width) : 0,
      wrapW: (() => { const w = document.querySelector('.reader-wrap'); return w ? Math.round(w.getBoundingClientRect().width) : 0; })(),
    });
  })()`;

  const { result: evaluated } = await send(
    "Runtime.evaluate",
    { expression: expr, returnByValue: true },
    s,
  );

  // 先滚到中间，再点「☰」：抽屉是覆盖式的，开合不应改变正文滚动位置（AC-19-4）
  await send(
    "Runtime.evaluate",
    { expression: `(() => { const v = document.getElementById('view'); if (v) v.scrollTop = 1200; return v ? v.scrollTop : -1; })()` },
    s,
  );
  await sleep(120);
  const { result: beforeOpen } = await send(
    "Runtime.evaluate",
    { expression: `document.getElementById('view')?.scrollTop ?? -1`, returnByValue: true },
    s,
  );
  await send(
    "Runtime.evaluate",
    { expression: `document.getElementById('btn-toc')?.click()` },
    s,
  );
  await sleep(420);
  const { result: drawerState } = await send(
    "Runtime.evaluate",
    {
      expression: `JSON.stringify({
        open: document.getElementById('drawer')?.classList.contains('open'),
        maskShown: !document.getElementById('drawer-mask')?.hidden,
        tabs: Array.from(document.querySelectorAll('#drawer .drawer-tab')).map(b => b.textContent),
        tocLinks: document.querySelectorAll('#drawer .toc-link').length,
        tocFirst: document.querySelector('#drawer .toc-link')?.textContent,
        width: Math.round(document.getElementById('drawer')?.getBoundingClientRect().width || 0),
        viewScrollAfter: document.getElementById('view')?.scrollTop ?? -1,
      })`,
      returnByValue: true,
    },
    s,
  );

  console.log("RESULT: " + JSON.stringify({ width, ...JSON.parse(evaluated.result.value) }));
  const drawerObj = JSON.parse(drawerState.result.value);
  drawerObj.scrollBeforeOpen = beforeOpen.result.value;
  console.log("DRAWER: " + JSON.stringify(drawerObj));
  if (errors.length) {
    console.log("JS ERRORS:", JSON.stringify(errors, null, 2));
  } else {
    console.log("JS ERRORS: none");
  }

  // 截图前先关掉抽屉，否则看到的是一半被遮住的正文
  await send(
    "Runtime.evaluate",
    { expression: `document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))` },
    s,
  );
  await sleep(320);

  const shotPath = process.argv[6];
  if (shotPath) {
    const { result: shot } = await send(
      "Page.captureScreenshot",
      { format: "png", captureBeyondViewport: false },
      s,
    );
    const fs = await import("node:fs");
    fs.writeFileSync(shotPath, Buffer.from(shot.data, "base64"));
    console.log("SHOT: " + shotPath);
  }

  await send("Target.closeTarget", { targetId: target.targetId });
  ws.close();
}

main().catch((err) => {
  console.error("uicheck 失败:", err);
  process.exit(1);
});
