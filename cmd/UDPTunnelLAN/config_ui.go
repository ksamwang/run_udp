package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"

	"udp_tunnel_demo/internal/lan"
)

type lanConfigHooks struct {
	Runtime        func() lanRuntimeInfo
	SaveConfig     func(lan.Config) (bool, error)
	RestartService func() error
}

type lanRuntimeInfo struct {
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	BuildTime     string `json:"build_time"`
	InstallPath   string `json:"install_path"`
	LogPath       string `json:"log_path"`
	ServiceStatus string `json:"service_status"`
}

func startLANConfigServer(cfg *lan.Config, configPath string, hooks lanConfigHooks) string {
	state := &lanConfigState{cfg: *cfg, path: configPath, hooks: hooks}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("LAN config UI disabled: %v", err)
		return ""
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", state.handlePage)
	mux.HandleFunc("/api/config", state.handleConfig)
	mux.HandleFunc("/api/runtime", state.handleRuntime)
	mux.HandleFunc("/api/restart-service", state.handleRestartService)
	go func() {
		if err := http.Serve(ln, mux); err != nil {
			log.Printf("LAN config UI stopped: %v", err)
		}
	}()
	url := "http://" + ln.Addr().String()
	log.Printf("LAN config UI listening on %s", url)
	return url
}

type lanConfigState struct {
	mu    sync.Mutex
	cfg   lan.Config
	path  string
	hooks lanConfigHooks
}

func (s *lanConfigState) handlePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, lanConfigHTML)
}

func (s *lanConfigState) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		writeLANJSON(w, s.cfg)
	case http.MethodPost:
		var req lan.Config
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.cfg.ServerHTTP = req.ServerHTTP
		s.cfg.LogLevel = req.LogLevel
		elevated := false
		var err error
		if s.hooks.SaveConfig != nil {
			elevated, err = s.hooks.SaveConfig(s.cfg)
		} else {
			err = lan.SaveConfig(s.path, s.cfg)
		}
		s.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeLANJSON(w, map[string]any{"ok": true, "restart_required": true, "elevation_required": elevated})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *lanConfigState) handleRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.hooks.Runtime != nil {
		writeLANJSON(w, s.hooks.Runtime())
		return
	}
	writeLANJSON(w, lanRuntimeInfo{Version: Version, Commit: Commit, BuildTime: BuildTime})
}

func (s *lanConfigState) handleRestartService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.hooks.RestartService == nil {
		http.Error(w, "restart unsupported", http.StatusNotImplemented)
		return
	}
	if err := s.hooks.RestartService(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeLANJSON(w, map[string]any{"ok": true})
}

func writeLANJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

const lanConfigHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>UDP Tunnel LAN 配置</title>
  <style>
    *{box-sizing:border-box} body{margin:0;font-family:Arial,"Microsoft YaHei",sans-serif;background:#f5f7fa;color:#1d2733}
    main{max-width:820px;margin:0 auto;padding:24px} h1{margin:0 0 18px}
    form{display:grid;grid-template-columns:repeat(2,minmax(220px,1fr));gap:14px;background:#fff;border:1px solid #d7dee7;border-radius:8px;padding:16px}
    label{display:grid;gap:6px;font-weight:700;font-size:14px} input{border:1px solid #b9c4d0;border-radius:6px;padding:9px 10px;min-height:38px}
    small{color:#607080;font-weight:400;line-height:1.4} button{border:1px solid #1769aa;background:#1769aa;color:white;border-radius:6px;padding:9px 13px;cursor:pointer}
    .full{grid-column:1/-1}.msg{min-height:22px;color:#1769aa}@media(max-width:720px){form{grid-template-columns:1fr}main{padding:14px}}
  </style>
</head>
<body>
<main>
  <h1>UDP Tunnel LAN 配置</h1>
  <form id="form">
    <label>控制面地址<input name="server_http" placeholder="http://api.tunnel.wanglv.top"><small>LAN 服务启动后从这里拉取虚拟网络、虚拟 IP、ACL 和 peer 信息。</small></label>
    <label>日志级别<input name="log_level" placeholder="info"><small>预留字段，当前版本主要用于保持配置格式。</small></label>
    <div class="full">
      <button type="submit">保存配置</button>
      <button type="button" id="restart-service">重启 LAN 服务</button>
      <p class="msg" id="msg"></p>
      <div id="runtime" style="display:grid;grid-template-columns:repeat(2,minmax(180px,1fr));gap:8px;margin-top:10px;color:#445260;font-size:14px"></div>
      <small>保存后需要重启 UDPTunnelLAN 服务才会生效。安装目录、日志和配置与端口转发客户端完全分离。</small>
    </div>
  </form>
</main>
<script>
const form=document.querySelector("#form"),msg=document.querySelector("#msg"),runtime=document.querySelector("#runtime");
async function load(){const r=await fetch("/api/config");const c=await r.json();for(const [k,v] of Object.entries(c)){const el=form.elements[k];if(!el)continue;el.value=String(v??"");}}
async function loadRuntime(){const r=await fetch("/api/runtime");const c=await r.json();runtime.innerHTML=[["版本",c.version],["提交",c.commit],["构建时间",c.build_time],["安装路径",c.install_path],["日志路径",c.log_path],["服务状态",c.service_status]].map(([k,v])=>"<div><strong>"+k+":</strong> "+String(v||"")+"</div>").join("");}
form.addEventListener("submit",async e=>{e.preventDefault();const fd=new FormData(form),body=Object.fromEntries(fd.entries());const r=await fetch("/api/config",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)});if(!r.ok){msg.textContent="保存失败："+await r.text();return;}const resp=await r.json();msg.textContent=resp.elevation_required?"已请求管理员权限保存配置，请确认 UAC 后再重启服务":"已保存，重启 LAN 服务后生效";});
document.querySelector("#restart-service").addEventListener("click",async()=>{const r=await fetch("/api/restart-service",{method:"POST"});msg.textContent=r.ok?"LAN 服务重启请求已发送":"LAN 服务重启失败："+await r.text();loadRuntime().catch(()=>{});});
Promise.all([load(),loadRuntime()]).catch(e=>msg.textContent=e);
</script>
</body>
</html>`
