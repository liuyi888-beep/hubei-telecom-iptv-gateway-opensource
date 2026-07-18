# 湖北电信 IPTV 网关 Go 版

一个轻量的湖北电信 IPTV 家庭网关服务。程序会自动登录 IPTV Portal，获取直播频道、EPG 节目单和回看入口，并输出常见播放器可订阅的播放源。

Go 版不依赖 Python、ffmpeg 或 CGO，Docker 最终运行镜像基于 `scratch`。

## 主要功能

- 自动登录 IPTV Portal，维护 UserToken 和 Cookie。
- 认证配置只需要填写最小参数，EPG/TVOD 基础地址由系统下发并写入数据库。
- 续登录使用轻量模式，只刷新会话和系统下发地址，不重新解析或覆盖频道列表。
- 自动获取动态频道、TimeShiftURL、FCC 和 EPG。
- 输出 DIYP、酷9、rtp2httpd 播放源。
- 输出 XMLTV：`/epg.xml`。
- RTSP 回看：`rtsp://网关:8555/catchup`，返回最终媒体节点 RTSP 302。
- rtp2httpd 回看：`/catchup.ts`，纯 Go 接收 RTSP UDP RTP/MPEG-TS。
- bbolt 持久缓存频道、节目单、认证状态、回看日志、TimeShiftURL、FCC 和系统下发 EPG 基础地址。

## 1.0.3 更新内容

- 修复重启后当前直播时移可能走 `getTVODPlayURL` 并失败的问题：频道缓存现在会保存 `TimeShiftURL`。
- rtp2httpd 播放源会保存并输出 FCC 参数。
- `/api/channels` 不再暴露 `timeshift_url` 和 URL 里的认证敏感参数。
- 续登录拆成轻量登录，只刷新会话和下发地址，不重新解析频道列表。
- HTTP 后端返回 4xx/5xx 时会作为错误处理，并对错误预览做脱敏。
- EPG/TVOD 基础地址必须来自系统下发或缓存，缺失时明确报错，不再回退旧配置字段。
- 节目单刷新加入保护阈值：开启保护时，如果新节目数量少于旧缓存一半，会保留旧缓存并报错。
- 修复 README、示例配置和频道分组里的中文编码问题。

## 1.0.2 更新内容

- 认证配置彻底精简，只读取 `user_id`、`password`、`stbid`、`mac`、`auth_ip`、`platform_base`。
- `epg_user_ip`、`dynamic_auth_ip`、`auth_base`、`easip_base`、`epg_base`、`easip`、`networkid`、`user_group_nmb`、`epg_group_nmb`、`stbtype`、`main_win_src` 等旧认证字段已不再读取。
- EPG/TVOD 基础地址改为认证时从系统下发页面自动发现，并写入数据库；后续认证收到新地址会自动覆盖。
- 酷9和 rtp2httpd M3U 对同名频道做显示名去重：第一个保留原名，后续重复项追加频道号，例如 `CCTV5HD`、`CCTV5HD [18]`。
- rtp2httpd M3U 支持输出 FCC 参数，例如 `rtp://239.253.64.120:5140/?fcc=121.60.255.120:15970`。

## 1.0.1 更新内容

- 移除 `startup_refresh`，自动刷新统一由 `background_refresh_enabled` 控制。
- 后台刷新器在 HTTP/RTSP 启动完成后加载。
- 手动强制刷新改为异步提交，页面不再长时间卡住。
- 刷新中防止重复提交；后台定时刷新遇到已有刷新任务时，300 秒后重试。
- `/api/channels` 中 `catchup` 简化为频道是否有可用 `rtsp://` TimeShiftURL。
- 移除刷新期 TVOD 能力探测相关配置和代码。

## Docker 部署

源码构建部署：

```bash
cp config/config.example.json config/config.json
nano config/config.json
docker compose up -d --build
```

预构建镜像部署：

[hubei-telecom-iptv-gateway-go-1.0.3.tar](https://github.com/liuyi888-beep/hubei-telecom-iptv-gateway-opensource/releases/download/v1.0.3/hubei-telecom-iptv-gateway-go-1.0.3.tar)

```bash
docker load -i hubei-telecom-iptv-gateway-go-1.0.3.tar
docker compose up -d
```

## 配置

首次使用：

```bash
cp config/config.example.json config/config.json
```

认证部分只需要填写：

```json
{
  "auth": {
    "enabled": true,
    "user_id": "YOUR_IPTV_USER_ID",
    "password": "YOUR_IPTV_PASSWORD",
    "stbid": "YOUR_STBID",
    "mac": "YOUR_STB_MAC",
    "auth_ip": "YOUR_IPTV_SERVICE_IP",
    "platform_base": "http://121.60.255.6:8080"
  }
}
```

`auth_ip` 是 IPTV 业务侧 IP，通常类似 `10.x.x.x`。实测填写 NAS 的局域网 IP 也可用，这个值一般不敏感。

## 常用地址

```text
http://网关IP:8899/
http://网关IP:8899/status.json
http://网关IP:8899/api/channels
http://网关IP:8899/ku9.m3u
http://网关IP:8899/rtp2httpd_catchup.m3u
http://网关IP:8899/diyp/live.txt
http://网关IP:8899/epg.xml
```

## 调试接口

```text
http://网关IP:8899/api/auth/status
http://网关IP:8899/refresh?force=1
http://网关IP:8899/catchup_debug?...参数...
http://网关IP:8899/catchup_log
```

`/refresh?force=1` 会立即返回：

- `202 Accepted`：刷新任务已提交。
- `409 Conflict`：已有刷新任务正在执行。

## 升级到 1.0.3

1.0.3 新增了 `TimeShiftURL` 和 FCC 缓存字段。建议升级后按你的计划清理旧 `data` 缓存，再启动服务重新刷新一次。

## 注意

本项目仅用于个人学习和家庭局域网自用。请遵守当地网络、运营商业务和账号使用规则。
