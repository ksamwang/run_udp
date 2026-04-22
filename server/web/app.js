const $ = (s) => document.querySelector(s);
let lastDevices = [];
const api = async (url, opts = {}) => {
  const res = await fetch(url, { headers: { "Content-Type": "application/json" }, ...opts });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
};

async function init() {
  try {
    await api("/api/me");
    showApp();
  } catch {
    $("#login").classList.remove("hidden");
  }
}

function showApp() {
  $("#login").classList.add("hidden");
  $("#app").classList.remove("hidden");
  refresh();
}

async function refresh() {
  const [metrics, devices, rules, sessions, settings] = await Promise.all([
    api("/api/metrics"), api("/api/devices"), api("/api/forwards"), api("/api/sessions"), api("/api/settings")
  ]);
  $("#metrics").innerHTML = [
    ["设备", metrics.devices], ["在线", metrics.online_devices],
    ["转发规则", metrics.forward_rules], ["中继字节", metrics.relay_bytes]
  ].map(([k, v]) => `<div class="metric">${k}<strong>${v}</strong></div>`).join("");
  updateDeviceSelects(devices);
  $("#device-list").innerHTML = devices.map(d => `<tr><td>${d.id}</td><td>${d.addr || ""}</td><td>${d.online ? "在线" : "离线"}</td><td>${d.last_seen || ""}</td></tr>`).join("");
  $("#rule-list").innerHTML = rules.map(r => `<tr><td>${r.name || r.id}</td><td>${r.source_id}:${r.local_port}</td><td>${r.target_id}</td><td>${r.target_host}:${r.target_port}</td><td>${r.enabled ? "启用" : "停用"}</td><td><button data-del="${r.id}">删除</button></td></tr>`).join("");
  $("#session-list").innerHTML = sessions.map(s => `<tr><td>${s.id}</td><td>${s.source_id} -> ${s.target_id}</td><td>${s.path}</td><td>${s.relay_bytes}</td><td>${s.last_seen}</td></tr>`).join("");
  updateSettings(settings);
}

function updateDeviceSelects(devices) {
  const signature = devices.map(d => `${d.id}:${d.online}`).join("|");
  if (signature === lastDevices.join("|")) return;
  lastDevices = devices.map(d => `${d.id}:${d.online}`);
  const options = devices.length
    ? devices.map(d => `<option value="${escapeHTML(d.id)}">${escapeHTML(d.id)}${d.online ? "（在线）" : "（离线）"}</option>`).join("")
    : `<option value="" disabled selected>暂无设备，先启动客户端 agent</option>`;
  ["#source-id", "#target-id"].forEach(sel => {
    const el = $(sel);
    const prev = el.value;
    el.innerHTML = options;
    if (devices.some(d => d.id === prev)) el.value = prev;
  });
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, ch => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;", "'": "&#39;"
  }[ch]));
}

$("#login-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  try {
    await api("/api/login", { method: "POST", body: JSON.stringify({ password: $("#password").value }) });
    showApp();
  } catch {
    $("#login-error").textContent = "登录失败";
  }
});

$("#logout").addEventListener("click", async () => {
  await api("/api/logout", { method: "POST", body: "{}" });
  location.reload();
});

document.querySelectorAll("nav button").forEach(btn => btn.addEventListener("click", () => {
  document.querySelectorAll("nav button,.tab").forEach(x => x.classList.remove("active"));
  btn.classList.add("active");
  $("#" + btn.dataset.tab).classList.add("active");
}));

$("#rule-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const fd = new FormData(e.currentTarget);
  const body = Object.fromEntries(fd.entries());
  body.local_port = Number(body.local_port);
  body.target_port = Number(body.target_port);
  body.enabled = fd.has("enabled");
  if (!body.source_id || !body.target_id) {
    alert("请先选择入口设备和出口设备。");
    return;
  }
  if (body.source_id === body.target_id) {
    alert("入口设备和出口设备不能相同。");
    return;
  }
  await api("/api/forwards", { method: "POST", body: JSON.stringify(body) });
  e.currentTarget.reset();
  $("#target-host").value = "127.0.0.1";
  refresh();
});

$("#rule-list").addEventListener("click", async (e) => {
  const id = e.target.dataset.del;
  if (!id) return;
  await api(`/api/forwards/${id}`, { method: "DELETE" });
  refresh();
});

function updateSettings(settings) {
  const form = $("#settings-form");
  if (document.activeElement && form.contains(document.activeElement)) return;
  form.elements.peer_ttl.value = settings.peer_ttl || "";
  form.elements.pair_ttl.value = settings.pair_ttl || "";
  form.elements.relay_idle_timeout.value = settings.relay_idle_timeout || "";
  form.elements.allow_relay.checked = !!settings.allow_relay;
  form.elements.allow_legacy.checked = !!settings.allow_legacy;
  $("#readonly-settings").innerHTML = [
    ["UDP 监听", settings.udp_listen],
    ["STUN 备用端口", settings.stun_alt_listen],
    ["HTTP 面板", settings.http_listen],
    ["数据库", settings.database_path],
    ["PSK", settings.psk_configured ? "已配置" : "未配置"]
  ].map(([k, v]) => `<div class="readonly-item"><span>${k}</span><strong>${escapeHTML(v || "")}</strong></div>`).join("");
}

$("#settings-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const fd = new FormData(e.currentTarget);
  const body = Object.fromEntries(fd.entries());
  body.allow_relay = fd.has("allow_relay");
  body.allow_legacy = fd.has("allow_legacy");
  try {
    await api("/api/settings", { method: "PATCH", body: JSON.stringify(body) });
    $("#settings-msg").textContent = "已保存";
    refresh();
  } catch (err) {
    $("#settings-msg").textContent = "保存失败：" + err.message;
  }
});

$("#password-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const fd = new FormData(e.currentTarget);
  const body = Object.fromEntries(fd.entries());
  try {
    await api("/api/admin/password", { method: "POST", body: JSON.stringify(body) });
    e.currentTarget.reset();
    $("#password-msg").textContent = "密码已修改";
  } catch (err) {
    $("#password-msg").textContent = "修改失败：" + err.message;
  }
});

setInterval(() => { if (!$("#app").classList.contains("hidden")) refresh().catch(() => {}); }, 5000);
init();
