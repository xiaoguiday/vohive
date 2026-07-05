# VoHive Docker Hub 镜像

镜像地址：`skyhotspur/vohive`

支持架构：

- `linux/amd64`
- `linux/arm64`

## 快速启动

```bash
mkdir -p vohive/{config,data,logs}
cd vohive
```

创建 `config/config.yaml`：

```yaml
server:
  port: 7575
  debug: false

web:
  username: admin
  password: admin

devices: []

proxy:
  instances: []

vowifi:
  enabled: false
```

创建 `docker-compose.yml`：

```yaml
services:
  vohive:
    image: skyhotspur/vohive:${VOHIVE_TAG:-1.5.5}
    container_name: vohive
    restart: unless-stopped
    network_mode: host
    privileged: true
    volumes:
      - ./config:/app/config
      - ./data:/app/data
      - ./logs:/app/logs
      - /dev:/dev
    environment:
      TZ: Asia/Shanghai
      CONFIG_PATH: /app/config/config.yaml
```

启动：

```bash
docker compose up -d
```

Web 入口：`http://YOUR_IP:7575`

默认账号：`admin` / `admin`

首次登录后请立即修改密码。

## 更新镜像

```bash
docker pull "skyhotspur/vohive:${VOHIVE_TAG:-1.5.5}"
docker compose up -d
```

应用内二进制热更新在这个源码整合构建中已禁用。Docker 部署请通过拉取新镜像升级。

## 配置说明

| 路径 | 说明 |
| --- | --- |
| `/app/config` | 配置文件目录 |
| `/app/data` | SQLite 数据与运行数据 |
| `/app/logs` | 日志目录 |

容器默认时区为 `Asia/Shanghai`。Compose 文件也显式设置了同一时区，方便在不同运行方式下保持一致。

## 许可证提示

本仓库是源码整合树，不是单一 MIT 许可项目。根项目来自 PolyForm Noncommercial 1.0.0，`third_party/vowifi-go` 为 AGPL-3.0，其它第三方源码按各自许可证授权。发布公开二进制或 Docker 镜像前，请先确认组合分发的许可证义务。
