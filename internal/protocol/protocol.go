package protocol

import (
	"encoding/json"
	"fmt"
)

// 消息类型
const (
	MsgRegister  = "register" // Client -> Server: 我叫 X，我要配对 Y
	MsgPeerInfo  = "peer"     // Server -> Client: 告诉你对方的公网地址
	MsgPunch     = "punch"    // Client -> Client: 打洞探测包
	MsgPunchAck  = "punchack" // Client -> Client: 打洞响应
	MsgData      = "data"     // Client -> Client: 业务数据
	MsgKeepAlive = "ka"       // Client -> Client: 保活
	MsgStunReq   = "stunq"    // Client -> Server: 请求观察到的公网地址
	MsgStunResp  = "stunr"    // Server -> Client: 你的公网地址是 X
)

type Message struct {
	Type     string `json:"t"`
	From     string `json:"f,omitempty"`  // 自己的 ID
	Name     string `json:"n,omitempty"`  // 设备显示名
	Peer     string `json:"p,omitempty"`  // 对端 ID
	Profile  string `json:"pr,omitempty"` // 隧道业务 profile
	Addr     string `json:"a,omitempty"`  // 公网地址 ip:port（NAT 观察到的）
	UpnpAddr string `json:"u,omitempty"`  // UPnP 主动映射出的公网地址 ip:port（可选）
	Payload  string `json:"d,omitempty"`  // 业务载荷
}

func Encode(m *Message) ([]byte, error) {
	return json.Marshal(m)
}

func Decode(b []byte) (*Message, error) {
	m := &Message{}
	if err := json.Unmarshal(b, m); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return m, nil
}
