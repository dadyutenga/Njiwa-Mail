/* Njiwa-Mail live chat widget. Add with one tag:
 *   <script src="https://YOUR-HOST/v1/chat/CHAT_ID/widget.js" async></script>
 * No cookies are set. A conversation is remembered in this browser's localStorage. */
(function () {
  "use strict";
  var el = document.currentScript;
  if (!el || !el.src) {
    var all = document.getElementsByTagName("script");
    for (var i = all.length - 1; i >= 0; i--) {
      if (/\/v1\/chat\/[^/]+\/widget\.js/.test(all[i].src)) { el = all[i]; break; }
    }
  }
  if (!el) return;
  var m = /^(https?:\/\/[^/]+)\/v1\/chat\/([0-9a-f-]{36})\/widget\.js/i.exec(el.src);
  if (!m || window.__njiwaChat === m[2]) return;
  window.__njiwaChat = m[2];
  var API = m[1] + "/v1/chat/" + m[2];
  var KEY = "njiwa-chat-" + m[2];

  var cfg = { title: "Chat", welcome: "", offline_message: "", announcement: "", links: [] };
  var conv = null; // { id, token }
  try { conv = JSON.parse(localStorage.getItem(KEY) || "null"); } catch (e) { conv = null; }
  var cursor = "", seen = {}, abort = null, poll = null, open = false, view = "home";

  function save() { try { if (conv) localStorage.setItem(KEY, JSON.stringify(conv)); else localStorage.removeItem(KEY); } catch (e) {} }
  function call(method, path, body, token) {
    var h = { "Content-Type": "application/json" };
    if (token) h.Authorization = "Bearer " + token;
    return fetch(API + path, { method: method, headers: h, body: body ? JSON.stringify(body) : undefined, mode: "cors" }).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (j) {
        if (!r.ok) { var e = new Error((j.error && (j.error.message || j.error.code)) || "Something went wrong"); e.status = r.status; throw e; }
        return j;
      });
    });
  }

  // ---- UI (Shadow DOM so the site's CSS cannot break it)
  var host = document.createElement("div");
  host.style.cssText = "position:fixed;right:20px;bottom:20px;z-index:2147483000;font:14px/1.4 system-ui,sans-serif";
  var root = host.attachShadow({ mode: "open" });
  root.innerHTML =
    '<style>' +
    '*{box-sizing:border-box}button,input,textarea{font:inherit}' +
    '.fab{width:56px;height:56px;border-radius:50%;border:0;background:#2563eb;color:#fff;cursor:pointer;box-shadow:0 4px 14px rgba(0,0,0,.25);font-size:24px}' +
    '.panel{display:none;position:absolute;right:0;bottom:70px;width:340px;max-width:calc(100vw - 40px);height:480px;max-height:calc(100vh - 110px);background:#fff;color:#111827;border-radius:14px;box-shadow:0 8px 30px rgba(0,0,0,.3);flex-direction:column;overflow:hidden}' +
    '.panel.on{display:flex}.head{background:#2563eb;color:#fff;padding:14px 16px;font-weight:600;display:flex;justify-content:space-between;align-items:center}' +
    '.head button{background:none;border:0;color:#fff;cursor:pointer;font-size:18px}' +
    '.body{flex:1;overflow:auto;padding:14px;display:flex;flex-direction:column;gap:8px}' +
    '.note{background:#eff6ff;border-radius:10px;padding:10px;color:#1e3a8a}.links a{display:block;padding:10px;border:1px solid #e5e7eb;border-radius:10px;color:#111827;text-decoration:none;margin-top:6px}' +
    '.links a:hover{background:#f3f4f6}.msg{max-width:85%;padding:8px 11px;border-radius:12px;white-space:pre-wrap;word-wrap:break-word}' +
    '.v{align-self:flex-end;background:#2563eb;color:#fff}.a{align-self:flex-start;background:#f3f4f6}.who{font-size:11px;color:#6b7280;margin:0 4px}' +
    '.foot{border-top:1px solid #e5e7eb;padding:10px;display:flex;gap:8px}.foot textarea{flex:1;resize:none;border:1px solid #d1d5db;border-radius:8px;padding:8px;height:40px}' +
    '.btn{background:#2563eb;color:#fff;border:0;border-radius:8px;padding:8px 14px;cursor:pointer}.btn.alt{background:#e5e7eb;color:#111827}' +
    'input{width:100%;border:1px solid #d1d5db;border-radius:8px;padding:8px;margin-top:6px}label{font-size:12px;color:#374151;display:block;margin-top:8px}' +
    '.err{color:#b91c1c;font-size:12px;margin-top:6px}.small{font-size:12px;color:#6b7280}' +
    '</style>' +
    '<button class="fab" aria-label="Chat">\u{1F4AC}</button>' +
    '<div class="panel" role="dialog" aria-label="Chat"><div class="head"><span id="t"></span><button id="x" aria-label="Close">×</button></div><div class="body" id="b"></div><div class="foot" id="f"></div></div>';
  var $ = function (id) { return root.getElementById(id); };
  var fab = root.querySelector(".fab"), panel = root.querySelector(".panel");

  function clear(n) { while (n.firstChild) n.removeChild(n.firstChild); }
  function h(tag, cls, text) { var e = document.createElement(tag); if (cls) e.className = cls; if (text !== undefined) e.textContent = text; return e; }
  function scroll() { var b = $("b"); b.scrollTop = b.scrollHeight; }

  function renderHome() {
    view = "home"; stopLive();
    $("t").textContent = cfg.title || "Chat";
    var b = $("b"), f = $("f"); clear(b); clear(f);
    if (cfg.announcement) b.appendChild(h("div", "note", cfg.announcement));
    if (cfg.welcome) b.appendChild(h("div", "", cfg.welcome));
    var ls = h("div", "links");
    (cfg.links || []).forEach(function (l) {
      if (!/^https?:\/\//i.test(l.url)) return;
      var a = h("a", "", l.label); a.href = l.url; a.target = "_blank"; a.rel = "noopener noreferrer"; ls.appendChild(a);
    });
    if (ls.childNodes.length) b.appendChild(ls);
    var go = h("button", "btn", conv ? "Continue the conversation" : "Start a chat");
    go.onclick = function () { conv ? renderChat() : renderStart(); };
    b.appendChild(go);
    if (cfg.offline_message) b.appendChild(h("div", "small", cfg.offline_message));
  }

  function renderStart() {
    view = "start";
    var b = $("b"), f = $("f"); clear(b); clear(f);
    b.appendChild(h("div", "", cfg.welcome || "Write to us. We answer here, and by email if you have left."));
    var name = h("input"); name.placeholder = "Name (optional)"; name.maxLength = 80; name.autocomplete = "name";
    var email = h("input"); email.type = "email"; email.placeholder = "Email (optional)"; email.autocomplete = "email";
    var msg = h("textarea"); msg.placeholder = "Your message"; msg.maxLength = 2000; msg.style.cssText = "width:100%;height:90px;margin-top:6px;border:1px solid #d1d5db;border-radius:8px;padding:8px";
    var hint = h("div", "small", "Leave an email address if you want the answer to reach you after you leave this page.");
    var err = h("div", "err"); var send = h("button", "btn", "Send");
    [h("label", "", "Name"), name, h("label", "", "Email"), email, h("label", "", "Message"), msg, hint, err, send].forEach(function (n) { b.appendChild(n); });
    send.onclick = function () {
      err.textContent = ""; send.disabled = true;
      call("POST", "/conversations", { name: name.value, email: email.value, message: msg.value }).then(function (r) {
        conv = { id: r.conversation_id, token: r.token }; save(); cursor = ""; seen = {}; renderChat();
      }).catch(function (e) { err.textContent = e.message; send.disabled = false; });
    };
  }

  function addMsg(x) {
    if (seen[x.id]) return; seen[x.id] = 1;
    var b = $("b");
    if (x.sender === "agent" && x.agent) b.appendChild(h("div", "who", x.agent));
    b.appendChild(h("div", "msg " + (x.sender === "visitor" ? "v" : "a"), x.body));
    scroll();
  }

  function renderChat() {
    view = "chat"; $("t").textContent = cfg.title || "Chat";
    var b = $("b"), f = $("f"); clear(b); clear(f); seen = {}; cursor = "";
    var ta = h("textarea"); ta.placeholder = "Write a message"; ta.maxLength = 2000;
    var send = h("button", "btn", "Send"); var back = h("button", "btn alt", "←");
    back.title = "Home"; back.onclick = renderHome;
    f.appendChild(back); f.appendChild(ta); f.appendChild(send);
    var go = function () {
      var v = ta.value.trim(); if (!v) return; send.disabled = true;
      call("POST", "/conversations/" + conv.id + "/messages", { body: v }, conv.token).then(function () { ta.value = ""; send.disabled = false; fetchNow(); })
        .catch(function (e) { send.disabled = false; if (e.status === 404) { conv = null; save(); renderStart(); } else { b.appendChild(h("div", "err", e.message)); scroll(); } });
    };
    send.onclick = go; ta.onkeydown = function (e) { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); go(); } };
    startLive();
  }

  // ---- live updates: a streamed connection, with plain polling as a fallback
  function take(items, cur) { (items || []).forEach(addMsg); if (cur) cursor = cur; }
  function fetchNow() {
    if (!conv) return Promise.resolve();
    return call("GET", "/conversations/" + conv.id + "/messages?after=" + encodeURIComponent(cursor), null, conv.token)
      .then(function (r) { take(r.items, r.cursor); }).catch(function (e) { if (e.status === 404) { conv = null; save(); renderStart(); } });
  }
  function stopLive() { if (abort) { abort.abort(); abort = null; } if (poll) { clearInterval(poll); poll = null; } }
  function startLive() {
    stopLive(); if (!conv || !open) return;
    fetchNow();
    if (!window.fetch || !window.ReadableStream || !window.AbortController) { poll = setInterval(fetchNow, 4000); return; }
    var ctl = new AbortController(); abort = ctl;
    (function connect() {
      if (ctl.signal.aborted || !conv) return;
      fetch(API + "/conversations/" + conv.id + "/stream?after=" + encodeURIComponent(cursor), { headers: { Authorization: "Bearer " + conv.token }, signal: ctl.signal, mode: "cors" })
        .then(function (r) {
          if (!r.ok || !r.body) throw new Error("stream");
          var rd = r.body.getReader(), dec = new TextDecoder(), buf = "";
          return (function pump() {
            return rd.read().then(function (x) {
              if (x.done) return;
              buf += dec.decode(x.value, { stream: true });
              var parts = buf.split("\n\n"); buf = parts.pop();
              parts.forEach(function (p) {
                var id = "", data = "", ev = "";
                p.split("\n").forEach(function (l) {
                  if (l.indexOf("id: ") === 0) id = l.slice(4); else if (l.indexOf("event: ") === 0) ev = l.slice(7); else if (l.indexOf("data: ") === 0) data += l.slice(6);
                });
                if (ev === "message" && data) { try { addMsg(JSON.parse(data)); if (id) cursor = id; } catch (e) {} }
              });
              return pump();
            });
          })();
        })
        .catch(function () {})
        .then(function () { if (!ctl.signal.aborted) setTimeout(connect, 3000); });
    })();
  }

  function toggle(v) {
    open = v; panel.classList.toggle("on", v);
    if (v) { if (view === "chat") startLive(); else if (view === "home") renderHome(); } else stopLive();
  }
  fab.onclick = function () { toggle(!open); };
  $("x").onclick = function () { toggle(false); };

  // Not shown at all if the chat is off, deleted, or this page's origin is not allowed.
  call("GET", "/config").then(function (c) {
    cfg = c; renderHome();
    if (document.body) document.body.appendChild(host);
    else document.addEventListener("DOMContentLoaded", function () { document.body.appendChild(host); });
  }).catch(function () {});
})();
