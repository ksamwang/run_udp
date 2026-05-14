const $ = (s) => document.querySelector(s);
let lastDevices = [];
const api = async (url, opts = {}) => {
  const res = await fetch(url, { headers: { "Content-Type": "application/json" }, ...opts });
  if (!res.ok) {
    let payload;
    try {
      payload = await res.json();
    } catch {
      throw new Error(await res.text());
    }
    const err = new Error(payload.error || "request failed");
    err.code = payload.code || "";
    throw err;
  }
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
  const [metrics, devices, rules, sessions, tunnelStates, settings] = await Promise.all([
    api("/api/metrics"), api("/api/devices"), api("/api/forwards"), api("/api/sessions"), api("/api/tunnel-states"), api("/api/settings")
  ]);
  $("#metrics").innerHTML = [
    ["设备", metrics.devices], ["在线", metrics.online_devices],
    ["转发规则", metrics.forward_rules], ["中继字节", metrics.relay_bytes]
  ].map(([k, v]) => `<div class="metric">${k}<strong>${v}</strong></div>`).join("");
  updateDeviceSelects(devices);
  $("#device-list").innerHTML = devices.map(d => `<tr>
    <td>${escapeHTML(d.name || "")}</td>
    <td>${escapeHTML(d.id)}</td>
    <td>${renderDeviceAddr(d)}</td>
    <td>${d.enabled ? (d.online ? "在线" : "离线") : "已禁用"}</td>
    <td>${escapeHTML(d.health_summary || "")}</td>
    <td>${escapeHTML(d.last_error || "")}</td>
    <td>${d.last_seen || ""}</td>
    <td>
      <button data-toggle-device="${escapeHTML(d.id)}" data-enabled="${d.enabled ? "1" : "0"}">${d.enabled ? "禁用" : "启用"}</button>
      <button data-del-device="${escapeHTML(d.id)}">删除</button>
    </td>
  </tr>`).join("");
  $("#rule-list").innerHTML = rules.map(r => `<tr>
    <td>${escapeHTML(r.name || String(r.id))}</td>
    <td>${profileLabel(r.profile)}</td>
    <td>${escapeHTML(r.source_id)}:${r.local_port}</td>
    <td>${escapeHTML(r.target_id)}</td>
    <td>${escapeHTML(r.target_host)}:${r.target_port}</td>
    <td>${escapeHTML(r.runtime_state || (r.enabled ? "down" : "disabled"))}</td>
    <td>${escapeHTML(r.last_error || "")}</td>
    <td>${escapeHTML(r.attempt ? String(r.attempt) : "")}</td>
    <td>${escapeHTML(r.next_retry_at || "")}</td>
    <td>${escapeHTML(r.last_updated_at || r.updated_at || "")}</td>
    <td><button data-del="${r.id}">删除</button></td>
  </tr>`).join("");
  const stateMap = buildTunnelStateMap(tunnelStates);
  $("#session-list").innerHTML = sessions.map(s => renderSessionRow(s, stateMap)).join("");
  updateSettings(settings);
}

function updateDeviceSelects(devices) {
  const enabledDevices = devices.filter(d => d.enabled);
  const signature = enabledDevices.map(d => `${d.id}:${d.online}:${d.enabled}`).join("|");
  if (signature === lastDevices.join("|")) return;
  lastDevices = enabledDevices.map(d => `${d.id}:${d.online}:${d.enabled}`);
  const options = enabledDevices.length
    ? enabledDevices.map(d => `<option value="${escapeHTML(d.id)}">${escapeHTML(d.name || d.id)} / ${escapeHTML(d.id)}${d.online ? "（在线）" : "（离线）"}</option>`).join("")
    : `<option value="" disabled selected>暂无可用设备，先启动并启用客户端 agent</option>`;
  ["#source-id", "#target-id"].forEach(sel => {
    const el = $(sel);
    const prev = el.value;
    el.innerHTML = options;
    if (enabledDevices.some(d => d.id === prev)) el.value = prev;
  });
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, ch => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;", "'": "&#39;"
  }[ch]));
}

function renderDeviceAddr(device) {
  const parts = [];
  if (device.addr) parts.push(`UDP/HTTP: ${escapeHTML(device.addr)}`);
  if (device.upnp_addr) parts.push(`UPnP: ${escapeHTML(device.upnp_addr)}`);
  return parts.join("<br>") || "";
}

function buildTunnelStateMap(states) {
  const map = new Map();
  for (const s of states) {
    map.set(pairKey(s.device_id, s.peer_id, s.profile), s);
  }
  return map;
}

function pairKey(a, b, profile) {
  return [...[a, b].sort(), normalizeProfile(profile)].join("\u0000");
}

function renderSessionRow(session, stateMap) {
  const profile = normalizeProfile(session.profile);
  const state = stateMap.get(pairKey(session.source_id, session.target_id, profile));
  const path = state?.via || session.path || "";
  const nat = state?.nat_type || "";
  const conv = state?.conv_id ? String(state.conv_id) : "";
  const rtt = state?.rtt_ms ? `${state.rtt_ms} ms` : "";
  const status = state?.state || "";
  const lastError = state?.last_error || "";
  const attempt = state?.attempt ? String(state.attempt) : "";
  const nextRetryAt = state?.next_retry_at || "";
  return `<tr>
    <td>${session.id}</td>
    <td>${escapeHTML(session.source_id)} -> ${escapeHTML(session.target_id)}</td>
    <td>${profileLabel(profile)}</td>
    <td>${escapeHTML(path)}</td>
    <td>${escapeHTML(nat)}</td>
    <td>${escapeHTML(status)}</td>
    <td>${escapeHTML(conv)}</td>
    <td>${escapeHTML(rtt)}</td>
    <td>${escapeHTML(attempt)}</td>
    <td>${escapeHTML(nextRetryAt)}</td>
    <td>${session.relay_bytes}</td>
    <td>${session.last_seen}<br><small>${escapeHTML(lastError)}</small></td>
  </tr>`;
}

function normalizeProfile(profile) {
  return profile === "bulk" ? "bulk" : "interactive";
}

function profileLabel(profile) {
  return normalizeProfile(profile) === "bulk" ? "文件传输" : "即时操作";
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
  try {
    await api("/api/forwards", { method: "POST", body: JSON.stringify(body) });
  } catch (err) {
    alert(`保存规则失败：${err.message}${err.code ? ` (${err.code})` : ""}`);
    return;
  }
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

$("#device-list").addEventListener("click", async (e) => {
  const toggleID = e.target.dataset.toggleDevice;
  if (toggleID) {
    const enabled = e.target.dataset.enabled === "1";
    try {
      await api(`/api/devices/${toggleID}`, { method: "PATCH", body: JSON.stringify({ enabled: !enabled }) });
      refresh();
    } catch (err) {
      alert(`更新设备失败：${err.message}${err.code ? ` (${err.code})` : ""}`);
    }
    return;
  }
  const deleteID = e.target.dataset.delDevice;
  if (!deleteID) return;
  try {
    await api(`/api/devices/${deleteID}`, { method: "DELETE" });
    refresh();
  } catch (err) {
    alert(`删除设备失败：${err.message}${err.code ? ` (${err.code})` : ""}`);
  }
});

function updateSettings(settings) {
  const form = $("#settings-form");
  if (document.activeElement && form.contains(document.activeElement)) return;
  form.elements.peer_ttl.value = settings.peer_ttl || "";
  form.elements.pair_ttl.value = settings.pair_ttl || "";
  form.elements.relay_idle_timeout.value = settings.relay_idle_timeout || "";
  form.elements.allow_relay.checked = !!settings.allow_relay;
  form.elements.allow_legacy.checked = !!settings.allow_legacy;
  form.elements.client_no_upnp.checked = !!settings.client_no_upnp;
  form.elements.client_upnp_timeout.value = settings.client_upnp_timeout || "";
  form.elements.client_log_level.value = settings.client_log_level || "";
  form.elements.client_tray_enabled.checked = !!settings.client_tray_enabled;
  form.elements.client_punch_timeout.value = settings.client_punch_timeout || "";
  form.elements.client_force_relay.checked = !!settings.client_force_relay;
  form.elements.client_allow_legacy.checked = !!settings.client_allow_legacy;
  form.elements.client_release_version.value = settings.client_release_version || "";
  form.elements.client_release_url.value = settings.client_release_url || "";
  form.elements.client_release_sha256.value = settings.client_release_sha256 || "";
  form.elements.client_release_published_at.value = settings.client_release_published_at || "";
  form.elements.client_release_notes.value = settings.client_release_notes || "";
  form.elements.client_release_minimum_supported_version.value = settings.client_release_minimum_supported_version || "";
  form.elements.client_release_file.value = settings.client_release_file || "";
  $("#readonly-settings").innerHTML = [
    ["UDP 监听", settings.udp_listen],
    ["STUN 备用端口", settings.stun_alt_listen],
    ["HTTP 面板", settings.http_listen],
    ["数据库", settings.database_path],
    ["PSK", settings.psk_configured ? "已配置" : "未配置"],
    ["客户端发布版本", settings.client_release_version || ""],
    ["客户端发布URL", settings.client_release_url || ""]
  ].map(([k, v]) => `<div class="readonly-item"><span>${k}</span><strong>${escapeHTML(v || "")}</strong></div>`).join("");
}

$("#settings-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const fd = new FormData(e.currentTarget);
  const body = Object.fromEntries(fd.entries());
  body.allow_relay = fd.has("allow_relay");
  body.allow_legacy = fd.has("allow_legacy");
  body.client_no_upnp = fd.has("client_no_upnp");
  body.client_tray_enabled = fd.has("client_tray_enabled");
  body.client_force_relay = fd.has("client_force_relay");
  body.client_allow_legacy = fd.has("client_allow_legacy");
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
