# 湖北电信 IPTV 网关 Go 版

一个轻量的湖北电信 IPTV 家庭网关服务。它自动登录 IPTV Portal，获取直播频道、EPG 节目单和回看入口，并输出常用播放器可订阅的播放源。

Go 版不依赖 Python、ffmpeg 或 CGO。Docker 最终运行镜像基于 `scratch`，容器内只有静态二进制。

## 主要功能

- 自动登录 IPTV Portal，维护 UserToken 和 Cookie。
- 会话失效时优先尝试 rebuildsession，失败后重新登录并重试原接口。
- 自动获取动态频道、TimeShiftURL 和 EPG。
- 输出 DIYP、酷9、rtp2httpd 播放源。
- 输出 XMLTV：`/epg.xml`。
- 酷9回看：`rtsp://网关:8555/catchup`，返回最终媒体节点 RTSP 302。
- rtp2httpd 回看：`/catchup.ts`，纯 Go 接收 RTSP UDP RTP/MPEG-TS。
- 4 小时内优先走频道 TimeShiftURL，历史节目按需走 getTVODPlayURL。
- bbolt 持久缓存频道、节目单、状态和回看日志。
- 首页提供状态面板，`/status.json` 提供详细 JSON 状态。

## 1.0.1 更新内容

- `startup_refresh` 已移除，只保留 `background_refresh_enabled` 作为自动刷新开关。
- 程序完全启动并监听 HTTP/RTSP 后，后台刷新器才会加载。
- 后台刷新按 `last_refresh + refresh_interval_hours` 计算下一次刷新；缓存过期或没有缓存时立即刷新。
- 手动强制刷新改为异步提交，接口立即返回，不再卡住浏览器。
- 刷新中会拒绝重复提交；后台定时刷新遇到已有刷新任务时，300 秒后重试。
- `/api/channels` 中 `catchup` 已简化：频道有可用 `rtsp://` TimeShiftURL 就是 `true`。
- 已移除刷新期 TVOD 能力探测逻辑、`detect_catchup_capability`、`catchup_detect_concurrency` 和 `timeshift_url_available`。

## Docker 部署

```bash
cp config/config.example.json config/config.json
nano config/config.json
docker compose up -d --build
```

默认端口：

```text
HTTP 8899
RTSP 8555
```

建议使用 host 网络，以保证 IPTV 专网、组播和 RTSP UDP 回流正常。

## 订阅地址

```text
http://网关IP:8899/
http://网关IP:8899/ku9.m3u
http://网关IP:8899/rtp2httpd_catchup.m3u
http://网关IP:8899/diyp/live.txt
http://网关IP:8899/epg.xml
```

## 调试接口

```text
http://网关IP:8899/status.json
http://网关IP:8899/api/auth/status
http://网关IP:8899/api/channels
http://网关IP:8899/refresh?force=1
http://网关IP:8899/catchup_debug?...参数...
http://网关IP:8899/catchup_log
```

`/refresh?force=1` 会立即返回：

- `202 Accepted`：刷新任务已提交。
- `409 Conflict`：已有刷新任务正在执行。

刷新状态可通过 `/status.json` 的 `refresh` 字段查看：

```json
{
  "running": true,
  "running_force": true,
  "started_at": "2026-07-13T11:30:00+08:00"
}
```

## 回看路径

```text
酷9 -> RTSP :8555/catchup -> RTSP 302 -> 电信最终媒体节点
HTTP 播放器 -> /catchup.ts -> RTSP SETUP/PLAY(UDP) -> RTP/MPEG-TS -> HTTP MPEG-TS
```

湖北电信最终节点实测不接受 RTSP interleaved TCP，因此 `/catchup.ts` 必须运行在能接收 IPTV 专网 UDP 回包的 NAS 或 host 网络环境中。

## 配置

主配置文件：

```text
config/config.json
```

首次使用：

```bash
cp config/config.example.json config/config.json
```

然后填写 IPTV 账号、密码、STBID、MAC 和业务 IP 等参数。

关键刷新配置：

```json
{
  "background_refresh_enabled": true,
  "refresh_interval_hours": 24
}
```

`background_refresh_enabled=true` 时，服务启动完成后会加载后台刷新器。如果缓存已过期或没有缓存，会立即刷新；否则等到下一次计划时间。

## 缓存与刷新

- 默认每 24 小时刷新一次。
- 非强制刷新会沿用 24 小时内的新鲜缓存。
- 手动强制刷新异步执行，首页按钮会自动防止重复点击。
- EPG 刷新失败或返回空节目时不会覆盖旧缓存。
- 后台定时刷新失败后 10 分钟重试。
- 后台定时刷新遇到已有刷新任务时 300 秒后重试。

## 目录说明

```text
*.go                       Go 源码
config/config.example.json 配置模板
Dockerfile                 多阶段构建，最终镜像为 scratch
docker-compose.yml         源码构建部署
data/                      运行时缓存目录，部署后自动生成
```

## 注意

本项目仅用于个人学习和家庭局域网自用。请遵守当地网络、运营商业务和账号使用规则。
