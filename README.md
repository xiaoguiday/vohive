# VoHive

4G/LTE/5G 模组管理平台，支持 Quectel 模组的代理、短信、eSIM、VoWiFi 及 CS 语音桥接。

## 特性

- **多模代理**: 支持 SOCKS5/HTTP 代理，流量统计
- **短信管理**: AT/QMI/MBIM 三模短信收发
- **eSIM 管理**: AT/QMI/MBIM 三模 eSIM 切换
- **VoWiFi**: IMS 承载的 Wi-Fi Calling
- **CS 语音桥接**: 通过 Linphone 软电话接打 4G 电路域电话（PCM ↔ RTP 音频桥接）

## 编译

### 前置条件

- Go 1.24+
- Node.js 20+
- 嵌入式 Web 前端需先构建

### 构建步骤

```bash
# 1. 构建前端
cd web
npm ci
npm run build
cp -r dist ../internal/web/

# 2. 构建后端
cd ..
GOWORK=off go build -trimpath -tags "with_utls nomsgpack" -o vohive ./cmd/vohive
```

### 交叉编译

```bash
# amd64
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOWORK=off go build ...

# arm64
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go build ...

# armv7
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 GOWORK=off go build ...
```

## GitHub Actions

推送代码或创建 tag（`v*`）即可自动编译 amd64 / arm64 / armv7 三种架构。Release 会自动附带编译好的二进制。

## 配置

参考 `config/config.example.yaml`，复制为 `config/config.yaml` 后修改。

### CS 语音桥接配置

```yaml
vowifi:
  voice_gateway:
    sip:
      listen: "0.0.0.0:5060"     # SIP 监听地址
      transport: "udp"
      realm: "vohive.local"
    media:
      rtp_port_min: 10000
      rtp_port_max: 20000
    users:
      - username: "linphone"
        password: "your_password"
        device_id: "device-1"
```

然后在 Linphone 上注册 SIP 账号即可通过 4G 模组接打电话。

## 免责声明

本软件仅供个人技术测试与研究交流，严禁任何商业用途或非法使用。
