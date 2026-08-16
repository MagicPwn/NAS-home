# NAS Home 本地服务发现入口开发 Spec

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.
>
> 本文是 `NAS Home` 的产品与开发规格，目标是先实现一个可部署、可验证的 Docker 容器服务，再逐步扩展管理能力。

**Goal:** 构建一个运行在 NAS 上的统一服务入口，按 10 秒周期读取本机 Docker 容器的名称、状态和已发布端口，为可确认的 Web 服务生成准确跳转链接，并在服务变化后自动刷新首页。

**Architecture:** 采用 Go 单体后端 + React/Vite 前端，后端通过受限的 Docker Engine API 读取容器信息，使用 10 秒全量 reconcile 作为权威状态源，辅以 Docker events 加速变化感知。服务发现只把 Docker 的 host published port 当作可访问端口；容器内部 `EXPOSE`、容器 IP 和未发布端口一律不能直接生成跳转链接。链接来源、可达性、推断程度和过期状态必须在 API 与 UI 中显式表达。

**Tech Stack:** Go、Docker Engine API、SQLite、React、TypeScript、Vite、CSS、Docker Compose；生产镜像使用多阶段构建，并将前端静态文件嵌入或由 Go 服务托管。

---

## 1. 产品命名与默认部署

### 1.1 名称

- 产品名称：`NAS Home`
- 仓库目录：`nas-home`
- Compose project：`nas-home`
- 主服务名：`nas-home`
- 容器名：`nas-home`
- 页面副标题：`NAS 本地服务入口`
- 一句话定位：`发现本机容器服务，打开所有入口。`

选择 `NAS Home` 而不是带有具体技术实现的名字，是为了让它以后可以纳入主机服务、反向代理服务和手工配置服务，而不被 Docker 绑定。第一版仍只实现本机 Docker 容器发现。

### 1.2 路径和端口

目标部署路径：

```text
/volume2/workspace/service/nas-home/
```

默认访问地址：

```text
http://<NAS_HOME_PUBLIC_HOST>:1111/
```

默认宿主机端口：`1111`，容器内部端口：`8080`。

当前 NAS 的 UGOS 系统 nginx 已占用 `9999`，因此默认使用 `1111`。部署脚本必须启动前检查 `1111` 是否被占用，并允许通过 `NAS_HOME_PORT` 覆盖，例如 `9180:8080`。

严禁把 `localhost` 或容器内部 hostname 当作用户浏览器的默认链接主机。必须通过环境变量配置外部可访问主机：

```env
NAS_HOME_PUBLIC_HOST=nas-host.example.invalid
NAS_HOME_PUBLIC_SCHEME=http
NAS_HOME_PORT=1111
```

当 NAS 使用域名时，改为域名；当局域网没有 DNS 时，使用 NAS 局域网 IP。自动读取容器 hostname 只能作为启动提示，不能作为“准确链接”的最终依据。

---

## 2. 范围

### 2.1 第一版必须实现

1. 读取本机 Docker daemon 的运行中容器。
2. 每 10 秒执行一次全量 reconcile，更新：
   - 容器 ID；
   - 容器名称；
   - 镜像名称；
   - 容器状态；
   - Compose project/service 信息；
   - Docker published ports；
   - host bind address；
   - 容器标签；
   - 首次发现、最后发现、最后探测时间。
3. 只使用 `NetworkSettings.Ports` 中的 host published TCP 端口生成自动链接。
4. 支持容器标签提供准确的显式 URL，优先级高于自动推断。
5. 对每个候选 Web 端口进行有限时长 HTTP/HTTPS 可达性探测。
6. 展示服务列表、容器状态、端口、链接来源和探测状态。
7. 支持按名称搜索、按分组筛选、按状态筛选和手工刷新。
8. 支持打开新标签页、复制链接、查看详情。
9. Docker API 暂时不可用时保留最后一次快照，并显示明确的 stale/error 状态，不把页面变成空白。
10. Docker Compose 容器化部署，支持数据持久化和自动重启。
11. 停止、重启、删除、重建容器后，入口列表在最多两个 reconcile 周期内保持一致。

### 2.2 第一版不实现

- 不通过入口页面启动、停止、重启或删除 Docker 容器。
- 不管理 Docker 镜像、网络、卷。
- 不扫描整个局域网或发现其他 NAS/主机。
- 不猜测任意 Web 应用的内部路径，例如 `/admin`、`/webui`。
- 不把仅 `EXPOSE`、仅 `expose`、容器 IP、Docker DNS 名称当作用户可访问地址。
- 不替代反向代理，不自动修改现有服务端口映射。
- 不通过浏览器自动化验证页面视觉效果。
- 不将服务密码、API Key、Cookie 或 Docker socket 写入数据库、日志或 API 响应。

---

## 3. 链接正确性契约

“有端口”不等于“有可用链接”。后端必须为每条链接记录来源和可信度，前端不得只显示一个看似可点击的字符串。

### 3.1 链接来源优先级

从高到低：

1. 数据库中的手工 override；
2. 容器标签 `nas.home.url`；
3. 容器标签指定 scheme/path + Docker published port；
4. Docker published TCP port + HTTP/HTTPS 探测成功；
5. 只发现端口但探测失败：显示端口，不生成默认可点击链接，除非用户显式确认；
6. 只有容器内部端口或 host network 无明确端口：不生成链接。

每条链接的 `source` 必须是：

```text
manual | label | published-port | host-network-inferred
```

第一版禁止把 `host-network-inferred` 自动标记为可用；host network 容器默认需要显式 `nas.home.url`。

### 3.2 published port 解析规则

对 Docker inspect 返回的 `NetworkSettings.Ports` 逐项处理：

- 仅处理 `tcp`；UDP/SCTP 只能作为端口信息展示，不能生成 HTTP 链接。
- 使用 `HostPort`，绝不能使用容器内部端口替代。
- `HostIp` 为 `0.0.0.0`、`::` 或空值时，使用配置的 `NAS_HOME_PUBLIC_HOST` 生成链接。
- `HostIp` 为 `127.0.0.1`、`::1` 时标记为 `local-only`，不得显示为对局域网用户可用。
- 其他明确 bind IP 使用该 bind IP 作为可达性事实，但默认链接仍使用 `NAS_HOME_PUBLIC_HOST`，避免用户从其他机器打开错误地址；详情页同时显示真实 bind address。
- 没有 host binding 的 `EXPOSE` 端口不生成链接。
- 同一容器多个 host port 必须合并为一个服务卡，并显示 primary link 与“更多端口”。

### 3.3 HTTP 探测规则

探测的目的只是判断端口是否提供 HTTP(S) 响应，不把探测结果当作授权检查：

- 首选 `HEAD /`；服务返回 405 或不支持 HEAD 时，用带响应体上限的 `GET /` fallback。
- 单次连接超时建议 1.5 秒，总超时不超过 3 秒。
- 不跟随跨主机重定向；将 3xx 记录为“服务有响应”。
- 2xx、3xx、401、403、404、405、429、500 等 HTTP 响应都表示端口有 HTTP 服务，应允许生成链接；只有连接失败、TLS 握手失败、协议不匹配或超时才视为未确认。
- 对候选端口分别尝试 HTTP 和 HTTPS 时必须有并发上限，避免 10 秒轮询造成 NAS 负载。
- 自动探测结果缓存 30 秒；Docker 端口变化时立即重新探测。
- 探测只针对当前 Docker published host port，不允许将任意标签 URL 直接交给后端请求，避免形成 SSRF。

### 3.4 URL 校验

显式 URL 必须：

- 仅允许 `http` 或 `https` scheme；
- 禁止用户名、密码、内嵌 token 和 fragment；
- 默认禁止 `javascript:`, `data:`, `file:` 等 scheme；
- 允许路径和查询参数；
- 显示前保留原始路径，不擅自改成另一个管理路径；
- 使用 Pydantic/Zod 或 Go URL parser 结构化校验，而不是字符串拼接后直接输出。

---

## 4. Docker 标签契约

标签是服务作者表达“正确入口”的首选方式。命名空间固定为 `nas.home.*`，避免与其他首页项目混用。

### 4.1 基本标签

```yaml
labels:
  nas.home.enabled: "true"
  nas.home.key: "arcreel"
  nas.home.name: "ArcReel"
  nas.home.group: "创作服务"
  nas.home.description: "AI 视频创作工作台"
  nas.home.icon: "film"
  nas.home.order: "20"
  nas.home.url: "http://nas-host.example.invalid:1241"
  nas.home.health_path: "/health"
```

语义：

- `enabled=false`：隐藏容器，但仍可在“已隐藏”筛选中查看；
- `key`：稳定服务 key，容器重建后保持 override 和历史关联；
- `name`：覆盖 Docker 容器名的显示名称；
- `group`：分组标题；
- `url`：完整、权威的 primary URL；设置后不再根据其他端口猜 primary URL；
- `health_path`：仅用于健康探测或详情显示，不改变点击 URL；
- `order`：同组内的数字排序。

### 4.2 端口级标签

单容器需要暴露多个入口时，第一版支持在配置文件中声明多链接；标签只声明 primary URL。避免在 Docker label 中嵌套 JSON，减少转义错误。

配置文件示例：

```yaml
services:
  arcreel:
    links:
      - name: 工作台
        url: http://nas-host.example.invalid:1241
        primary: true
      - name: 开发前端
        url: http://nas-host.example.invalid:5173
        primary: false
```

配置文件只用于补充标签无法表达的路径，不能覆盖容器实际不存在的 published port 而静默显示为“已发现”。详情页要标记 `manual-config`，并提示用户自行确认。

### 4.3 稳定 key 推导

稳定 key 的优先级：

1. `nas.home.key`；
2. `com.docker.compose.project` + `com.docker.compose.service`；
3. Docker container name；
4. container ID 作为最后 fallback。

容器 ID 不能作为正常配置 key，因为重建后会变化。

---

## 5. 服务状态模型

后端使用 SQLite 保存手工覆盖、隐藏状态、首次/最近观察和最近一次错误；实时容器状态仍以 Docker reconcile 结果为准。

### 5.1 `discovered_services`

字段至少包括：

```text
service_key
container_id
container_name
image
compose_project
compose_service
container_state
is_running
is_paused
is_stale
labels_json
published_ports_json
service_type
page_title
primary_url
links_json
link_source
reachability
local_only
last_seen_at
last_probe_at
last_probe_status
last_error
created_at
updated_at
```

### 5.2 `service_overrides`

```text
service_key
name_override
group_override
description_override
icon_override
url_override
links_json
hidden
sort_order
service_type
created_at
updated_at
```

override 必须按 `service_key` 关联，不能按短生命周期的 container ID 关联。

### 5.3 状态枚举

```text
running
paused
restarting
exited
created
unknown
```

链接可达性枚举：

```text
reachable
responding-authenticated
responding-error
unconfirmed
local-only
not-published
invalid
```

页面需要明确区分：

- 容器在运行，但没有 Web 入口；
- 有 published port，但尚未确认 HTTP；
- 有 HTTP 响应，但需要登录；
- 入口可点击，但 Docker 最近一次同步已过期。

### 5.4 全局设置、自定义 Tab 与外部链接

SQLite 另外保存：

```text
app_settings(setting_key, setting_value, updated_at)
custom_tabs(id, name, sort_order, created_at, updated_at)
custom_links(id, tab_id, url, name, description, icon, page_title,
             reachability, last_probe_at, last_probe_status, last_error,
             sort_order, created_at, updated_at)
```

`app_settings.mock_ip:<tabId>` 为空时，该 Tab 的 published-port 链接使用 `NAS_HOME_PUBLIC_HOST`；非空时，API 返回给该 Tab 跳转使用的 URL 将该 host 替换为 mock IP，数据库仍保留原始域名。设置为 localhost 时，该 Tab 的服务端探测跳过。

自定义 Tab 只允许用户维护，不参与 Docker reconcile；自定义链接支持任意 `http`/`https` URL，URL 是唯一必填字段。创建和更新时自动执行 HTTP 探测；用户填写的名称优先，没有名称时使用页面 `<title>`，仍没有 title 时回退 URL。

---

## 6. 后端架构与轮询机制

目标新项目目录：

```text
nas-home/
├── backend/
│   ├── cmd/nas-home/main.go
│   ├── internal/config/
│   ├── internal/docker/
│   ├── internal/discovery/
│   ├── internal/links/
│   ├── internal/store/
│   ├── internal/httpapi/
│   └── internal/health/
├── frontend/
│   ├── src/
│   ├── package.json
│   └── pnpm-lock.yaml
├── migrations/
├── deploy/
│   ├── compose.yml
│   ├── compose.override.example.yml
│   ├── .env.example
│   └── README.md
├── Dockerfile
├── Makefile
├── README.md
└── LICENSE
```

### 6.1 Discovery loop

实现 `Reconciler`：

1. 启动时立即 reconcile 一次，不等待 10 秒；
2. 每 10 秒调用 Docker API 获取运行中容器列表和 inspect 信息；
3. 以 container ID 做本轮快照去重；
4. 解析名称、标签、Compose 元数据和 published ports；
5. 应用 override；
6. 对新增/端口变化/标签变化的链接立即探测；
7. 更新 SQLite 和内存快照；
8. 发布 `services.updated` 事件；
9. Docker API 失败时保留上一次快照，写入错误和 stale 时间；
10. 下一轮成功后清除 stale 状态。

同时实现可选 `EventWatcher`：订阅 Docker events，收到 start/stop/die/rename/network/connect/destroy 等事件后触发一次去抖 reconcile。事件不是权威数据源，不能替代 10 秒全量 reconcile。

### 6.2 并发与资源限制

- Docker inspect 并发上限建议 8；
- HTTP 探测并发上限建议 8；
- 单次 reconcile 有总超时；
- 任何单个异常不能中断全量扫描；
- 日志只记录 container ID 前 12 位、service key 和错误类型，不打印完整 label JSON，防止意外泄漏敏感信息；
- 对 Docker API 做连接复用和超时设置；
- 数据库写入按快照批量事务提交，避免每个端口独立写盘。

### 6.3 Docker 权限设计

默认部署优先使用受限 Docker socket proxy，而不是直接把宿主机 socket 暴露给业务应用：

```text
nas-home -> read-only Docker API proxy -> /var/run/docker.sock
```

proxy 只开放容器列表、容器 inspect、events、version/info 等读取接口，不开放 create/start/stop/exec/kill/remove/build 等控制接口。镜像必须锁定 digest，并在部署测试中确认控制接口不可用。

如果 MVP 为减少组件而直接挂载 `/var/run/docker.sock`，必须在文档中明确：Docker socket 的文件挂载 `:ro` 不能把 Docker API 变成真正的只读权限，持有 socket 的应用具有接近 root 的 Docker 控制能力。直接 socket 仅作为开发模式，不作为默认生产部署。

---

## 7. HTTP API 契约

API 前缀：`/api/v1`。

### 7.1 系统接口

```text
GET /api/health
GET /api/v1/status
POST /api/v1/reconcile
GET /api/v1/events/stream
GET /api/v1/navigation
GET/PATCH /api/v1/settings
```

`/api/health` 只反映 NAS Home 进程可用；`/api/v1/status` 另外返回 Docker API、最后 reconcile、数据新鲜度和版本信息。

### 7.2 服务接口

```text
GET    /api/v1/services
GET    /api/v1/services/:serviceKey
PATCH  /api/v1/services/:serviceKey/override
DELETE /api/v1/services/:serviceKey/override
POST   /api/v1/services/:serviceKey/probe
```

自定义导航接口：

```text
GET    /api/v1/navigation
POST   /api/v1/custom-tabs
PATCH  /api/v1/custom-tabs/:id
DELETE /api/v1/custom-tabs/:id
POST   /api/v1/custom-tabs/:id/links
GET    /api/v1/custom-links/:id
PATCH  /api/v1/custom-links/:id
DELETE /api/v1/custom-links/:id
POST   /api/v1/custom-links/:id/probe
```

`GET /services` 支持：

```text
q=<名称或容器名>
group=<分组>
state=running|stopped|all
reachable=true|false
include_hidden=true|false
sort=name|group|last_seen|order
```

响应中每个服务至少返回：

```json
{
  "serviceKey": "arcreel",
  "name": "ArcReel",
  "containerName": "arcreel-dev",
  "containerState": "running",
  "group": "创作服务",
  "primaryLink": {
    "url": "http://nas-host.example.invalid:1241",
    "label": "打开服务",
    "source": "label",
    "reachability": "reachable",
    "localOnly": false
  },
  "publishedPorts": [
    {
      "hostIp": "0.0.0.0",
      "hostPort": 1241,
      "containerPort": 1241,
      "protocol": "tcp"
    }
  ],
  "lastSeenAt": "...",
  "lastProbeAt": "...",
  "stale": false
}
```

服务排序、链接来源和 stale 判断必须由后端完成，前端不能自己重建 URL。

### 7.3 SSE 与轮询

第一版前端每 10 秒调用 `GET /api/v1/services` 已足够可靠；后端 SSE 作为实时加速，不作为唯一更新机制。SSE 断开后自动回退到 10 秒轮询。

---

## 8. 前端页面规格

### 8.1 首页

页面结构：

1. 顶部品牌区：`NAS Home`、最后同步时间、Docker 状态、手动刷新；
2. 概览条：运行中容器数、可访问服务数、待确认端口数、异常数；
3. 搜索框：搜索显示名、容器名、镜像名、Compose service；
4. 分组筛选：全部、按 `nas.home.group` 和 Compose project；
5. 服务列表：分为“前台服务”和“后台服务”两个 Tab，默认打开前台服务；无 published TCP port 的容器默认归类为后台服务，用户可通过 override 手工修改服务类型；
6. 每张服务卡：图标、探测到的页面 title（无 title 时回退显示名）、描述、所属容器名、状态、所有有 HTTP 响应的端口链接（按 host port 升序）、来源 badge、最近探测；
7. 已停止/无链接服务折叠区；
8. Docker 不可用或数据 stale 时显示顶部告警，不隐藏旧数据。
9. Tab 区保留前台/后台两个内置 Tab，并支持用户新建、重命名、删除自定义 Tab；自定义 Tab 内展示任意外部 HTTP/HTTPS 链接。
10. 设置面板提供全局 mock IP；自定义链接维护表单允许只填 URL，也允许补全名称、描述、图标和排序。

### 8.2 交互

- “打开服务”使用后端返回的完整 URL，在新标签页打开；
- “复制链接”使用 Clipboard API，失败时显示可选文本；
- primary link 不可用时按钮 disabled，但详情中仍显示原因；
- 401/403 服务仍允许打开；
- local-only 服务显示“仅 NAS 本机可访问”，不伪装成局域网可用；
- no published port 服务显示“未发布端口”，不显示假链接；
- 端口变化、容器重建、名称变化后 UI 自动更新；
- 长容器名和长 URL 必须可折叠，页面不能横向溢出；
- 支持 `prefers-reduced-motion`，不使用持续高消耗动画。
- 自定义链接卡片支持打开、复制、编辑、删除和重新探测；探测结果显示页面 title、HTTP 状态和可达性。

### 8.3 详情抽屉

详情显示：

- 容器完整名称、镜像、Compose project/service；
- 所有 published ports；
- 每条 link 的来源和推断过程；
- bind address；
- 最近三次探测结果；
- 标签提供的可用元数据，但默认隐藏原始敏感/高噪声标签；
- override 编辑入口；
- “重新探测”按钮。

第一版 override 编辑只允许改名称、描述、分组、图标、URL、服务类型、隐藏和排序，不提供容器控制按钮。

---

## 9. 配置与部署

### 9.1 `.env.example`

```env
NAS_HOME_PORT=1111
NAS_HOME_PUBLIC_HOST=nas-host.example.invalid
NAS_HOME_PUBLIC_SCHEME=http
NAS_HOME_POLL_INTERVAL=10s
NAS_HOME_PROBE_TIMEOUT=3s
NAS_HOME_DATA_DIR=/data
NAS_HOME_LOG_LEVEL=info
DOCKER_HOST=unix:///var/run/docker.sock
```

生产部署不把凭据放入容器标签。若以后启用入口认证，增加：

```env
NAS_HOME_AUTH_MODE=basic
NAS_HOME_AUTH_USERNAME=admin
NAS_HOME_AUTH_PASSWORD_FILE=/run/secrets/nas_home_password
```

密码使用 Docker secret 或外部文件，不写入 Compose YAML 和 Git。

### 9.2 Compose 约束

`deploy/compose.yml` 至少包含：

- 服务名 `nas-home`；
- `1111:8080`，可由 env 覆盖；
- `restart: unless-stopped`；
- `/data` 持久化卷或绑定目录；
- healthcheck：`GET /api/health`；
- `init: true`；
- `no-new-privileges:true`；
- 尽量使用非 root 用户运行；
- 根文件系统只读，仅 `/data` 可写；
- Docker socket proxy 或明确标注的开发直挂模式；
- 不使用 `network_mode: host`；
- 应用到宿主机探测需要 `host.docker.internal:host-gateway`，但该地址只用于后端探测，不输出给浏览器。

### 9.3 启动前检查

提供 `make preflight` 或 `scripts/preflight.sh`：

1. 检查 Docker daemon 可用；
2. 检查 `/var/run/docker.sock` 或 proxy 可用；
3. 检查 `NAS_HOME_PORT` 未被占用；
4. 检查 `NAS_HOME_PUBLIC_HOST` 非 `localhost`，除非明确使用本机模式；
5. 执行 `docker compose config --quiet`；
6. 检查数据目录可写；
7. 输出不包含 secret 值。

---

## 10. 测试与验收标准

### 10.1 后端单元测试

至少覆盖：

- Docker name 清洗和名称优先级；
- stable service key 推导；
- `0.0.0.0`、`::`、`127.0.0.1`、`::1` bind address；
- host port 与 container port 不混用；
- UDP、未 published 的 EXPOSE 不生成链接；
- 多端口合并与 primary 选择；
- label URL 优先于自动推断；
- manual override 优先于 label；
- `nas.home.enabled=false` 隐藏逻辑；
- 401/403/404/500 视为有 HTTP 响应；
- timeout/TLS/connection refused 状态；
- invalid URL 被拒绝；
- Docker API 失败保留快照并进入 stale；
- 容器重建后 stable key 仍能加载 override；
- 单容器异常不会中止全量 reconcile；
- 空列表与 Docker daemon 无权限有不同错误。

### 10.2 Docker 集成测试

使用临时 Compose fixture 创建：

1. 一个 HTTP 服务并发布 host port；
2. 一个发布多个 TCP 端口的服务；
3. 一个只有 `EXPOSE` 没有 host port 的服务；
4. 一个 bind 到 `127.0.0.1` 的服务；
5. 一个带 `nas.home.url` 和稳定 key 的服务；
6. 一个被 `nas.home.enabled=false` 隐藏的服务；
7. 一个返回 401 的服务。

验收：

- 启动后 10 秒内发现；
- 停止后最多 20 秒内状态更新；
- 重建容器后 override 不丢；
- no-published 服务没有假链接；
- label URL 原样保留 path；
- 401 服务仍然可打开；
- Docker socket proxy 不能执行容器控制 API。

### 10.3 前端测试

- 服务卡显示状态、名称、链接来源；
- 搜索、分组、状态过滤；
- stale 顶部警告；
- local-only/no-published/invalid link 文案；
- 打开链接使用后端返回 URL；
- 复制链接成功和失败 fallback；
- SSE 断线后恢复轮询；
- 长名称/长 URL 不造成横向溢出；
- empty state 和 Docker error state。

### 10.4 部署验收

```bash
cd /volume2/workspace/service/nas-home
./scripts/preflight.sh
docker compose config --quiet
docker compose up -d --build
curl -fsS http://127.0.0.1:1111/api/health
curl -fsS http://127.0.0.1:1111/api/v1/status
curl -fsS http://127.0.0.1:1111/api/v1/services
```

必须验证：

- 容器为 `healthy`；
- `/api/health` 返回 200；
- `/api/v1/services` 返回合法 JSON；
- 至少一个现有 Docker Web 服务显示正确 host port；
- 页面中没有 `localhost:<service-port>` 这类错误链接；
- 修改/重建测试容器后两个轮询周期内更新；
- `docker compose restart` 后数据和 override 保留；
- 停止 NAS Home 后没有影响被发现的其他服务。

---

## 11. 开发阶段与交付顺序

### Phase 0：建立项目骨架

**Files:**

- Create: `/volume2/workspace/service/nas-home/backend/cmd/nas-home/main.go`
- Create: `/volume2/workspace/service/nas-home/frontend/package.json`
- Create: `/volume2/workspace/service/nas-home/deploy/compose.yml`
- Create: `/volume2/workspace/service/nas-home/deploy/.env.example`
- Create: `/volume2/workspace/service/nas-home/Dockerfile`
- Create: `/volume2/workspace/service/nas-home/README.md`

**验证:** Go 编译、前端 typecheck、Compose config 均通过。

### Phase 1：Docker 读取层

实现 `internal/docker`：

- client 初始化和超时；
- list running containers；
- inspect container；
- parse names/labels/ports；
- Docker unavailable 错误模型。

先写 parser 单元测试，再接真实 daemon。

### Phase 2：Reconciler 与 SQLite

实现 10 秒全量 reconcile、事件去抖、快照事务写入、stale 保留和 stable key。先验证容器 start/stop/recreate 场景。

### Phase 3：链接解析和可达性探测

实现来源优先级、URL 校验、bind address 处理、HTTP/HTTPS probe、并发上限和 reachability 状态。补齐所有链接契约测试。

### Phase 4：HTTP API

实现服务列表、详情、override、手工 probe、status 和 SSE。生成 OpenAPI 或等价接口文档，并为响应模型写 contract tests。

### Phase 5：前端首页

先实现 loading/error/empty/stale 状态，再实现列表、分组、搜索、详情抽屉、打开/复制链接。前端不允许自行拼接服务 URL。

### Phase 6：标签与 override

支持 `nas.home.*` 标签、SQLite override 和 stable key 重建恢复。完成手工配置路径和权限边界。

### Phase 7：安全部署

接入受限 Docker socket proxy，收紧 Compose 权限，加入 preflight、healthcheck、数据目录、日志轮转和无 secret 输出验证。

### Phase 8：真实 NAS 验收

在 `/volume2/workspace/service/nas-home/` 构建当前 checkout，启动 `nas-home`，使用至少三个现有容器服务核对：容器名、published host port、生成 URL、浏览器打开结果和服务状态。验收结论必须区分：

- Docker 元数据准确；
- URL 生成准确；
- HTTP 端口有响应；
- 页面实际可打开。

---

## 12. 风险与决策

### 风险：自动猜测主机名导致跳转错误

**决策:** `NAS_HOME_PUBLIC_HOST` 必须配置；自动 hostname 只做提示并显示 warning。

### 风险：把内部端口误当外部端口

**决策:** 没有 `HostPort` 就没有自动链接；`EXPOSE` 仅展示为内部端口。

### 风险：service 根路径不是正确入口

**决策:** 第一版不猜管理路径。使用 `nas.home.url` 或 SQLite override 明确指定完整路径。

### 风险：Docker socket 赋予过高权限

**决策:** 生产默认 socket proxy；开发直挂必须单独标明风险，不开放控制操作。

### 风险：10 秒探测带来负载

**决策:** Docker 元数据每 10 秒 reconcile；HTTP 仅对新增/变化端口立即探测，普通状态探测缓存 30 秒，并限制并发和响应体。

### 风险：服务重建后 override 丢失

**决策:** 使用 `nas.home.key` 或 Compose project/service 作为稳定 key，禁止以 container ID 作为正常配置主键。

### 风险：登录保护被误判为不可用

**决策:** 401/403/3xx 都是有响应的服务，UI 显示“需要登录”或“已响应”，仍允许用户点击。

---

## 13. 完成定义

只有以下条件全部满足，才可称为 NAS Home 第一版完成：

- [ ] 新项目目录与 Compose project 均为 `nas-home`；
- [ ] 默认宿主端口为 1111，并支持 preflight 和 env 覆盖；
- [ ] 10 秒全量 reconcile 已实现并有测试；
- [ ] Docker name、state、published port 解析有单元和集成测试；
- [ ] 未发布端口不会生成假链接；
- [ ] `127.0.0.1`/`::1` 入口不会伪装成局域网入口；
- [ ] label/manual/auto 三类链接来源可追溯；
- [ ] 服务路径不会被自动猜测；
- [ ] Docker API 失败保留快照并展示 stale；
- [ ] Docker socket proxy 或明确的开发权限边界已验收；
- [ ] 首页搜索、筛选、详情、打开、复制均可用；
- [ ] 至少三个真实 NAS 容器服务完成 URL 对照验收；
- [ ] `docker compose config --quiet`、后端测试、前端测试、类型检查、生产构建和部署 smoke test 全部通过；
- [ ] README 包含启动、停止、配置 public host、标签契约、权限风险和故障排查。

---

## 14. 推荐的第一步

先不要直接扫描并生成链接。先实现并验证“published port → bind address → public host → URL source”的纯函数和 Docker fixture。

这是整个产品最关键的正确性边界：

```text
Docker inspect
  -> host published port
  -> bind address classification
  -> configured NAS public host
  -> explicit/auto link resolution
  -> reachability state
  -> frontend anchor
```

这条链路通过后，再实现视觉和管理功能，避免先做出一个看起来完整、但会把内部端口或 localhost 错误暴露给用户的入口页面。
