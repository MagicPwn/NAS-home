# NAS Home

NAS Home 是 NAS 本地服务发现入口：读取本机 Docker 容器元数据，基于真实 published host port 生成入口，并将 Docker 状态、链接来源和可达性展示在一个首页中。

## 当前实现

- Go 后端：Docker 列表/inspect、10 秒全量 reconcile、SQLite 快照、stale 保留、稳定 service key。
- 默认通过独立的 `docker-socket-proxy` 读取 Docker；Web 应用本身不挂载 Docker socket。
- socket proxy 仅允许 Docker `GET /_ping`、`/version`、`/info`、`/containers/json` 和单容器 inspect，拒绝控制 API。
- 链接契约：只处理 published TCP 端口；`EXPOSE`、容器 IP 和未发布端口不会生成浏览器链接；`nas.home.url` 和 SQLite manual override 优先。
- HTTP 探测：自动端口只探测后端生成的 published-port 地址，HEAD 失败用 GET fallback，限制超时、不跟随重定向，并缓存 30 秒；401/403/4xx 仍被记录为有响应。
- API：`/api/health`、`/api/v1/status`、`/api/v1/services`、详情、override、手动 reconcile、手动 probe 和 SSE endpoint。
- React/Vite 首页：搜索、分组/状态/可达性筛选、已隐藏筛选、状态概览、打开/复制链接、详情抽屉、最近三次探测和重新探测。
- Docker Compose 默认端口为 `9080:8080`，SQLite 数据在 `/data` 持久化，NAS Home 以 UID 10001 运行。

## 启动

```bash
cd /volume2/workspace/service/NAS-home
cp deploy/.env.example deploy/.env
# 修改 NAS_HOME_PUBLIC_HOST：使用 NAS IP、局域网 DNS 或实际域名，不要使用 localhost
./scripts/preflight.sh
docker compose --env-file deploy/.env -f deploy/compose.yml up -d --build
curl -fsS http://127.0.0.1:9080/api/health
curl -fsS http://127.0.0.1:9080/api/v1/status
curl -fsS http://127.0.0.1:9080/api/v1/services
```

停止：

```bash
docker compose --env-file deploy/.env -f deploy/compose.yml down
```

用其他端口：`NAS_HOME_PORT=9180`，然后执行 `docker compose ... up -d`；preflight 会检查端口占用。

## Docker 标签

可选标签示例：

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
```

`nas.home.url` 只允许 `http`/`https`，不允许凭据、fragment 或其他危险 scheme。标签 URL 原样保留路径和查询参数，后端不会把它交给 HTTP 探测器，也不会猜 `/admin`、`/webui` 等内部路径。

## 链接规则

1. SQLite manual override。
2. `nas.home.url` 标签。
3. published TCP host port + 后端 HTTP 探测。
4. 只有内部端口、EXPOSE 或无 host binding：显示端口信息但不生成链接。
5. `127.0.0.1`/`::1`：显示“仅 NAS 本机可访问”，不会伪装成局域网入口。
6. `0.0.0.0`、`::` 或空 bind address：使用 `NAS_HOME_PUBLIC_HOST`，而不是 localhost 或容器内部地址。

## Docker socket 权限边界

默认 Compose 使用独立的 `docker-socket-proxy` 服务。只有 proxy 容器挂载 `/var/run/docker.sock`，NAS Home 通过内部 TCP 网络访问 proxy。proxy 的代码级 allowlist 只转发只读容器元数据接口，拒绝 `start`、`stop`、`exec`、`kill`、`remove`、`build` 等控制操作。

proxy 容器仍然是高权限基础设施组件，必须与 NAS Home 分开看待；生产镜像应继续锁定基础镜像 digest，并在部署验收中验证控制接口返回 403。若开发环境必须直挂 socket，应显式修改 Compose override 并理解 Docker socket 接近 root 权限，不能把 `:ro` 文件挂载误认为 API 只读。

## 开发验证

环境如果没有宿主 Go，可使用项目构建镜像或临时 Go SDK；前端需要 Node 22/npm：

```bash
cd backend
go test ./...
go vet ./...
go build ./cmd/nas-home
cd ../deploy/socket-proxy
go test .
cd ../../frontend
npm install
npm run typecheck
npm run build
cd ..
docker compose --env-file deploy/.env.example -f deploy/compose.yml config --quiet
```

后端单元测试覆盖 bind address、稳定 key、host/container port 区分、UDP/未发布端口、label URL 优先级、隐藏标签和 HTTP probe fallback。真实 NAS 验收还需使用临时 Compose fixture 检查 start/stop/recreate、401 服务、socket proxy 控制接口和至少三个现有 Web 服务的实际打开结果。

## API 查询参数

`GET /api/v1/services` 支持：

```text
q=<名称、容器、镜像或 Compose service>
group=<分组>
state=running|stopped|all
reachable=true|false
include_hidden=true|false
sort=name|group|last_seen|order
```

手动重新探测：

```bash
curl -X POST http://127.0.0.1:9080/api/v1/services/<serviceKey>/probe
```

## 故障排查

- 页面为空但顶部有 stale 告警：先看 `/api/v1/status`，NAS Home 会保留上一次快照，不会把 Docker 暂时不可用变成空白。
- 服务只有“未发布端口”：检查 Compose 的 `ports:`，只有 `expose:` 不足以生成浏览器链接。
- 链接打开失败：核对 `NAS_HOME_PUBLIC_HOST` 是否能被访问该页面的客户端解析；该值不是容器 hostname，也不能写 `localhost`。
- proxy unhealthy：确认宿主 `/var/run/docker.sock` 存在、源类型是 Unix socket，并查看 `docker-socket-proxy` 日志；不要把 socket 内容写入日志。
- 没有服务：确认 proxy 能访问 Docker，并检查 `/api/v1/status` 的错误信息。单个 inspect 失败时，旧服务会保留并标记 stale，完整下一轮成功后才清理真正消失的容器。
