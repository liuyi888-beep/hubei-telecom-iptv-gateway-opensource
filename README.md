# 湖北电信 IPTV 网关 Go 版

一个轻量的湖北电信 IPTV 家庭网关服务。它自动登录 IPTV Portal，获取直播频道、EPG 节目单和回看地址，并输出常用播放器可订阅的播放源。

Go 版不依赖 Python、ffmpeg 或 CGO。Docker 运行镜像使用 `scratch`，容器内只有静态二进制。

## 主要功能

- 自动登录 IPTV Portal，维护 UserToken 和 Cookie。
- 会话失效时先尝试 rebuildsession，失败后完整重新登录并重试原接口。
- 自动获取动态频道、TimeShiftURL 和 EPG。
- 输出 DIYP、酷9、rtp2httpd 播放源。
- 输出 XMLTV：`/epg.xml`。
- 酷9回看：`rtsp://网关:8555/catchup`，返回最终媒体节点 RTSP 302。
- rtp2httpd 回看：`/catchup.ts`，纯 Go 接收 RTSP UDP RTP/MPEG-TS，不调用 ffmpeg。
- 4 小时内优先走频道 TimeShiftURL，历史节目自动走 getTVODPlayURL。
- bbolt 持久缓存频道、节目单、状态和回看日志。
- 首页提供轻量状态面板，`/status.json` 提供详细 JSON 状态。

## Docker 部署

```bash
cp config/config.example.json config/config.json
nano config/config.json
docker compose up -d --build
```

服务默认端口：

```text
HTTP 8899
RTSP 8555
```

使用 host 网络是为了保证 IPTV 专网、组播和 RTSP UDP 回流正常。

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

## 缓存与刷新

- 默认启动时刷新一次。
- 24 小时内非强制刷新会沿用旧缓存。
- EPG 刷新失败或返回空节目时不会覆盖旧缓存。
- `background_refresh_enabled=false` 时不会启动后台定时刷新。

## 目录说明

```text
*.go                  Go 源码
config/config.example.json  配置模板
Dockerfile            多阶段构建，最终镜像为 scratch
docker-compose.yml    源码构建部署
data/                 运行时缓存目录，部署后自动生成
```

## 注意

本项目仅用于个人学习和家庭局域网自用。请遵守当地网络、运营商业务和账号使用规则。

