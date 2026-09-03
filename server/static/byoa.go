package static

import "strings"

// byoaVisitorScript 是 BYOA MVP 的最小访客端补丁。
// 不修改 OpenList 前端源码，只监听 /api/fs/get 的结构化 NEED_AUTH 响应，
// 弹出对应网盘二维码。扫码成功后刷新页面，用户再次点击播放即可。
//
// 设计原则：
// - 不读取网盘凭据；凭据由后端写入 HttpOnly Cookie。
// - 不引入用户系统、Redis 或服务端 Session。
// - 只注入访客页面，不注入管理后台。
const byoaVisitorScript = `<script data-xiaoya-byoa="mvp">
(function () {
  if (window.__xiaoyaByoaInstalled) return;
  window.__xiaoyaByoaInstalled = true;

  var active = null;
  var pollTimer = null;

  function apiRootFromURL(raw) {
    try {
      var u = new URL(raw || location.href, location.href);
      var marker = "/api/";
      var i = u.pathname.indexOf(marker);
      if (i >= 0) return u.pathname.slice(0, i);
    } catch (_) {}
    var path = location.pathname || "/";
    var manage = path.indexOf("/@manage");
    if (manage >= 0) path = path.slice(0, manage);
    return path.replace(/\/$/, "");
  }

  function requestJSON(url, options) {
    options = options || {};
    options.credentials = "same-origin";
    return fetch(url, options).then(function (r) {
      return r.json();
    }).then(function (body) {
      if (!body || body.code !== 200) {
        throw new Error((body && body.message) || "请求失败");
      }
      return body.data;
    });
  }

  function ensureModal() {
    var existed = document.getElementById("xy-byoa-overlay");
    if (existed) return existed;

    var overlay = document.createElement("div");
    overlay.id = "xy-byoa-overlay";
    overlay.innerHTML =
      '<div id="xy-byoa-card">' +
        '<button id="xy-byoa-close" type="button" aria-label="关闭">×</button>' +
        '<div id="xy-byoa-title">扫码后即可播放</div>' +
        '<div id="xy-byoa-subtitle"></div>' +
        '<img id="xy-byoa-qr" alt="扫码二维码" />' +
        '<div id="xy-byoa-status">二维码生成中...</div>' +
        '<button id="xy-byoa-retry" type="button">重新生成二维码</button>' +
      '</div>';

    var style = document.createElement("style");
    style.textContent =
      '#xy-byoa-overlay{position:fixed;inset:0;z-index:2147483647;background:rgba(0,0,0,.62);display:flex;align-items:center;justify-content:center;padding:20px;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}' +
      '#xy-byoa-card{position:relative;width:min(360px,92vw);box-sizing:border-box;background:#fff;color:#171717;border-radius:16px;padding:28px 24px 24px;text-align:center;box-shadow:0 18px 60px rgba(0,0,0,.3)}' +
      '#xy-byoa-close{position:absolute;right:12px;top:8px;border:0;background:transparent;font-size:30px;line-height:1;color:#666;cursor:pointer}' +
      '#xy-byoa-title{font-size:20px;font-weight:700;margin-bottom:8px}' +
      '#xy-byoa-subtitle{font-size:14px;color:#666;margin-bottom:18px}' +
      '#xy-byoa-qr{display:none;width:260px;height:260px;max-width:100%;margin:0 auto 14px;border-radius:8px;background:#fff}' +
      '#xy-byoa-status{font-size:14px;line-height:1.6;color:#555;min-height:24px}' +
      '#xy-byoa-retry{display:none;margin:14px auto 0;border:0;border-radius:9px;padding:10px 16px;background:#111;color:#fff;cursor:pointer}';
    document.head.appendChild(style);
    document.body.appendChild(overlay);

    overlay.querySelector("#xy-byoa-close").onclick = closeModal;
    overlay.querySelector("#xy-byoa-retry").onclick = function () {
      if (active) beginScan(active.provider, active.apiRoot);
    };
    return overlay;
  }

  function closeModal() {
    if (pollTimer) {
      clearTimeout(pollTimer);
      pollTimer = null;
    }
    var overlay = document.getElementById("xy-byoa-overlay");
    if (overlay) overlay.remove();
    active = null;
  }

  function providerName(provider) {
    return provider === "aliyun" ? "阿里云盘" : provider === "quark" ? "夸克网盘" : provider;
  }

  function showStatus(text, retry) {
    var status = document.getElementById("xy-byoa-status");
    var btn = document.getElementById("xy-byoa-retry");
    if (status) status.textContent = text;
    if (btn) btn.style.display = retry ? "inline-block" : "none";
  }

  function beginScan(provider, apiRoot) {
    if (pollTimer) clearTimeout(pollTimer);
    active = { provider: provider, apiRoot: apiRoot };
    var modal = ensureModal();
    modal.querySelector("#xy-byoa-title").textContent = "扫码后即可播放";
    modal.querySelector("#xy-byoa-subtitle").textContent = "请使用" + providerName(provider) + " App 扫描二维码";
    var img = modal.querySelector("#xy-byoa-qr");
    img.style.display = "none";
    img.removeAttribute("src");
    showStatus("二维码生成中...", false);

    var startURL = apiRoot + "/api/public/byoa/" + provider + "/start";
    requestJSON(startURL).then(function (data) {
      if (!active || active.provider !== provider) return;
      if (!data || !data.qr_image) throw new Error("二维码生成失败");
      img.src = data.qr_image;
      img.style.display = "block";
      showStatus("等待扫码...", false);
      poll(provider, apiRoot, data);
    }).catch(function (err) {
      showStatus(err && err.message ? err.message : "二维码生成失败", true);
    });
  }

  function poll(provider, apiRoot, session) {
    if (!active || active.provider !== provider) return;
    var query;
    if (provider === "quark") {
      query = "token=" + encodeURIComponent(session.token || "");
    } else {
      query = "ck=" + encodeURIComponent(session.ck || "") + "&t=" + encodeURIComponent(session.t || "");
    }
    var url = apiRoot + "/api/public/byoa/" + provider + "/status?" + query;
    requestJSON(url).then(function (data) {
      if (!active || active.provider !== provider) return;
      var status = data && data.status;
      if (status === "success") {
        showStatus("授权成功，正在刷新页面...", false);
        pollTimer = setTimeout(function () { location.reload(); }, 700);
        return;
      }
      if (status === "expired") {
        showStatus("二维码已过期", true);
        return;
      }
      if (status === "canceled") {
        showStatus("已取消授权", true);
        return;
      }
      if (status === "scanned") {
        showStatus("已扫码，请在手机上确认...", false);
      } else {
        showStatus("等待扫码...", false);
      }
      pollTimer = setTimeout(function () { poll(provider, apiRoot, session); }, 2000);
    }).catch(function (err) {
      showStatus(err && err.message ? err.message : "查询扫码状态失败", true);
    });
  }

  function inspectResponse(xhr) {
    try {
      if (!xhr.__xyByoaURL || xhr.__xyByoaURL.indexOf("/api/fs/get") < 0) return;
      var body = JSON.parse(xhr.responseText || "{}");
      var data = body && body.data;
      if (!data || data.byoa_auth_required !== true || !data.provider) return;
      if (data.provider !== "aliyun" && data.provider !== "quark") return;
      beginScan(data.provider, apiRootFromURL(xhr.__xyByoaURL));
    } catch (_) {}
  }

  var originalOpen = XMLHttpRequest.prototype.open;
  var originalSend = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.open = function (method, url) {
    this.__xyByoaURL = typeof url === "string" ? url : String(url || "");
    return originalOpen.apply(this, arguments);
  };
  XMLHttpRequest.prototype.send = function () {
    this.addEventListener("loadend", function () { inspectResponse(this); });
    return originalSend.apply(this, arguments);
  };
})();
</script>`

func injectBYOAVisitorScript(html string) string {
	if strings.Contains(html, `data-xiaoya-byoa="mvp"`) {
		return html
	}
	if strings.Contains(html, "</body>") {
		return strings.Replace(html, "</body>", byoaVisitorScript+"</body>", 1)
	}
	return html + byoaVisitorScript
}
