package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"udp_tunnel_demo/internal/config"
)

func startClientConfigServer(cfg *config.Client, configPath string) string {
	state := &clientConfigState{cfg: *cfg, path: configPath}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("client config UI disabled: %v", err)
		return ""
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", state.handlePage)
	mux.HandleFunc("/api/config", state.handleConfig)
	go func() {
		if err := http.Serve(ln, mux); err != nil {
			log.Printf("client config UI stopped: %v", err)
		}
	}()
	url := "http://" + ln.Addr().String()
	log.Printf("client config UI listening on %s", url)
	return url
}

type clientConfigState struct {
	mu   sync.Mutex
	cfg  config.Client
	path string
}

func (s *clientConfigState) handlePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, clientConfigHTML)
}

func (s *clientConfigState) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		writeClientJSON(w, s.cfg)
	case http.MethodPost:
		var req struct {
			Server       string `json:"server"`
			ServerHTTP   string `json:"server_http"`
			DeviceID     string `json:"device_id"`
			PSK          string `json:"psk"`
			NoUPnP       bool   `json:"no_upnp"`
			UPnPTimeout  string `json:"upnp_timeout"`
			LogLevel     string `json:"log_level"`
			TrayEnabled  bool   `json:"tray_enabled"`
			PunchTimeout string `json:"punch_timeout"`
			ForceRelay   bool   `json:"force_relay"`
			AllowLegacy  bool   `json:"allow_legacy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		upnpTimeout, err := time.ParseDuration(req.UPnPTimeout)
		if err != nil {
			http.Error(w, "bad upnp_timeout", http.StatusBadRequest)
			return
		}
		punchTimeout, err := time.ParseDuration(req.PunchTimeout)
		if err != nil {
			http.Error(w, "bad punch_timeout", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.cfg.Server = req.Server
		s.cfg.ServerHTTP = req.ServerHTTP
		s.cfg.DeviceID = req.DeviceID
		if s.cfg.DeviceID == "" {
			s.cfg.DeviceID = defaultDeviceID()
		}
		s.cfg.PSK = req.PSK
		s.cfg.NoUPnP = req.NoUPnP
		s.cfg.UPnPTimeout = upnpTimeout
		s.cfg.LogLevel = req.LogLevel
		s.cfg.TrayEnabled = req.TrayEnabled
		s.cfg.PunchTimeout = punchTimeout
		s.cfg.ForceRelay = req.ForceRelay
		s.cfg.AllowLegacy = req.AllowLegacy
		err = config.SaveJSON(s.path, s.cfg)
		s.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeClientJSON(w, map[string]any{"ok": true, "restart_required": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeClientJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

const clientConfigHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>UDP Tunnel 客户端配置</title>
  <style>
    *{box-sizing:border-box} body{margin:0;font-family:Arial,"Microsoft YaHei",sans-serif;background:#f5f7fa;color:#1d2733}
    main{max-width:820px;margin:0 auto;padding:24px} h1{margin:0 0 18px}
    form{display:grid;grid-template-columns:repeat(2,minmax(220px,1fr));gap:14px;background:#fff;border:1px solid #d7dee7;border-radius:8px;padding:16px}
    label{display:grid;gap:6px;font-weight:700;font-size:14px} input{border:1px solid #b9c4d0;border-radius:6px;padding:9px 10px;min-height:38px}
    small{color:#607080;font-weight:400;line-height:1.4} .check{display:flex;gap:8px;align-items:center}
    button{border:1px solid #1769aa;background:#1769aa;color:white;border-radius:6px;padding:9px 13px;cursor:pointer}
    .full{grid-column:1/-1}.msg{min-height:22px;color:#1769aa}@media(max-width:720px){form{grid-template-columns:1fr}main{padding:14px}}
  </style>
</head>
<body>
<main>
  <h1>UDP Tunnel 客户端配置</h1>
  <form id="form">
    <label>服务器 UDP 地址<input name="server" placeholder="121.41.66.128:7000"><small>客户端打洞和中继使用的公网 UDP 地址。</small></label>
    <label>控制面地址<input name="server_http" placeholder="http://121.41.66.128:7001"><small>Web 管理页和规则拉取 API 地址。</small></label>
    <label>设备 ID<input name="device_id"><small>留空时自动使用 Windows 计算机名。</small></label>
    <label>预共享密钥<input name="psk"><small>必须和服务端 PSK 一致。</small></label>
    <label>UPnP 超时<input name="upnp_timeout" placeholder="4s"><small>例如 4s。路由器不支持时会自动忽略。</small></label>
    <label>打洞超时<input name="punch_timeout" placeholder="30s"><small>超时后自动切换到 TURN 中继。</small></label>
    <label>日志级别<input name="log_level" placeholder="info"><small>当前支持记录配置，后续可扩展分级日志。</small></label>
    <label class="check"><input name="no_upnp" type="checkbox"> 禁用 UPnP</label>
    <label class="check"><input name="tray_enabled" type="checkbox"> 启用托盘</label>
    <label class="check"><input name="force_relay" type="checkbox"> 强制中继</label>
    <label class="check"><input name="allow_legacy" type="checkbox"> 允许旧明文协议</label>
    <div class="full"><button type="submit">保存配置</button><p class="msg" id="msg"></p><small>保存后需要重启客户端 agent 才会完全生效。</small></div>
  </form>
</main>
<script>
const form=document.querySelector("#form"),msg=document.querySelector("#msg");
async function load(){const r=await fetch("/api/config");const c=await r.json();for(const [k,v] of Object.entries(c)){const el=form.elements[k];if(!el)continue;if(el.type==="checkbox")el.checked=!!v;else el.value=String(v??"");}}
form.addEventListener("submit",async e=>{e.preventDefault();const fd=new FormData(form),body=Object.fromEntries(fd.entries());for(const n of ["no_upnp","tray_enabled","force_relay","allow_legacy"])body[n]=form.elements[n].checked;const r=await fetch("/api/config",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)});msg.textContent=r.ok?"已保存，重启客户端后生效":"保存失败："+await r.text();});
load().catch(e=>msg.textContent=e);
</script>
</body>
</html>`
