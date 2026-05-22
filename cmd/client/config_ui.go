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

type clientConfigHooks struct {
	OnSaved        func()
	SaveConfig     func(config.Client) (bool, error)
	Runtime        func() clientRuntimeInfo
	RestartService func() error
	CheckUpdates   func() error
}

func startClientConfigServer(cfg *config.Client, configPath string, hooks clientConfigHooks) string {
	state := &clientConfigState{cfg: *cfg, path: configPath, hooks: hooks}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("client config UI disabled: %v", err)
		return ""
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", state.handlePage)
	mux.HandleFunc("/api/config", state.handleConfig)
	mux.HandleFunc("/api/runtime", state.handleRuntime)
	mux.HandleFunc("/api/restart-service", state.handleRestartService)
	mux.HandleFunc("/api/check-updates", state.handleCheckUpdates)
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
	mu    sync.Mutex
	cfg   config.Client
	path  string
	hooks clientConfigHooks
}

type clientLocalConfigView struct {
	ServerHTTP string `json:"server_http"`
	DeviceName string `json:"device_name"`
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
		writeClientJSON(w, clientLocalConfigView{
			ServerHTTP: s.cfg.ServerHTTP,
			DeviceName: s.cfg.DeviceName,
		})
	case http.MethodPost:
		var req clientLocalConfigView
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.cfg.ServerHTTP = req.ServerHTTP
		s.cfg.DeviceName = req.DeviceName
		if s.cfg.DeviceName == "" {
			s.cfg.DeviceName = defaultDeviceName()
		}
		s.clearServerManagedConfig()
		elevated := false
		var err error
		if s.hooks.SaveConfig != nil {
			elevated, err = s.hooks.SaveConfig(s.cfg)
		} else {
			err = config.SaveClientLocalJSON(s.path, s.cfg)
		}
		s.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		processExiting := s.hooks.OnSaved != nil && !elevated
		writeClientJSON(w, map[string]any{
			"ok":                 true,
			"restart_required":   true,
			"process_exiting":    processExiting,
			"elevation_required": elevated,
		})
		if processExiting {
			go func() {
				time.Sleep(500 * time.Millisecond)
				s.hooks.OnSaved()
			}()
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *clientConfigState) clearServerManagedConfig() {
	s.cfg.Server = ""
	s.cfg.PeerID = ""
	s.cfg.PSK = ""
	s.cfg.NoUPnP = false
	s.cfg.UPnPTimeout = 0
	s.cfg.LogLevel = ""
	s.cfg.TrayEnabled = true
	s.cfg.PunchTimeout = 0
	s.cfg.ForceRelay = false
	s.cfg.AllowLegacy = false
	s.cfg.Forwards = nil
}

func (s *clientConfigState) handleRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.hooks.Runtime == nil {
		writeClientJSON(w, clientRuntimeInfo{})
		return
	}
	writeClientJSON(w, s.hooks.Runtime())
}

func (s *clientConfigState) handleRestartService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.hooks.RestartService == nil {
		http.Error(w, "restart unavailable", http.StatusNotImplemented)
		return
	}
	if err := s.hooks.RestartService(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeClientJSON(w, map[string]any{"ok": true})
}

func (s *clientConfigState) handleCheckUpdates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.hooks.CheckUpdates == nil {
		http.Error(w, "update unavailable", http.StatusNotImplemented)
		return
	}
	if err := s.hooks.CheckUpdates(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeClientJSON(w, map[string]any{"ok": true})
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
    <label>控制面地址<input name="server_http" placeholder="http://tunnel.example.com"><small>客户端唯一必填入口，启动后会从这里拉取运行配置。</small></label>
    <label>设备显示名<input name="device_name" placeholder="默认使用 Windows 计算机名"><small>Web 管理页优先显示这个名称。</small></label>
    <div class="full">
      <button type="submit">保存配置</button>
      <button type="button" id="restart-service">重启服务</button>
      <button type="button" id="check-updates">检查更新</button>
      <p class="msg" id="msg"></p>
      <div id="runtime" style="display:grid;grid-template-columns:repeat(2,minmax(180px,1fr));gap:8px;margin-top:10px;color:#445260;font-size:14px"></div>
      <small>保存后需要重启客户端 agent 才会完全生效；UDP 地址、打洞、UPnP 等运行参数由服务端统一下发。</small>
    </div>
  </form>
</main>
<script>
const form=document.querySelector("#form"),msg=document.querySelector("#msg"),runtime=document.querySelector("#runtime");
async function load(){const r=await fetch("/api/config");const c=await r.json();for(const [k,v] of Object.entries(c)){const el=form.elements[k];if(!el)continue;if(el.type==="checkbox")el.checked=!!v;else el.value=String(v??"");}}
async function loadRuntime(){const r=await fetch("/api/runtime");const c=await r.json();runtime.innerHTML=[["版本",c.version],["提交",c.commit],["构建时间",c.build_time],["安装路径",c.install_path],["日志路径",c.log_path],["服务状态",c.service_status],["更新状态",c.update_status],["上次检查",c.last_update_check],["最近错误",c.last_update_error]].map(([k,v])=>"<div><strong>"+k+":</strong> "+String(v||"")+"</div>").join("");}
form.addEventListener("submit",async e=>{e.preventDefault();const fd=new FormData(form),body=Object.fromEntries(fd.entries());const r=await fetch("/api/config",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)});if(!r.ok){msg.textContent="保存失败："+await r.text();return;}const resp=await r.json();msg.textContent=resp.elevation_required?"已请求管理员权限保存配置，请确认 UAC 后再重启服务":(resp.process_exiting?"已保存，客户端正在重启":"已保存，重启客户端后生效");if(resp.process_exiting){Array.from(form.elements).forEach(el=>el.disabled=true);}});
document.querySelector("#restart-service").addEventListener("click",async()=>{const r=await fetch("/api/restart-service",{method:"POST"});msg.textContent=r.ok?"服务重启请求已发送":"服务重启失败："+await r.text();loadRuntime().catch(()=>{});});
document.querySelector("#check-updates").addEventListener("click",async()=>{const r=await fetch("/api/check-updates",{method:"POST"});msg.textContent=r.ok?"更新检查已触发":"更新检查失败："+await r.text();loadRuntime().catch(()=>{});});
Promise.all([load(),loadRuntime()]).catch(e=>msg.textContent=e);
</script>
</body>
</html>`
