# 湖北电信 IPTV 网关 Go

轻量的家庭 IPTV 网关服务。程序登录 IPTV Portal，获取频道、EPG、时移和回看入口，并输出常见播放器可订阅的播放列表。

## 1.0.4 更新内容

- 认证流程收敛为唯一的 `login()`：首次登录、会话过期后的重新登录使用同一套协议。
- 登录只建立会话并发现系统下发的 EPG/TVOD 地址，不解析频道、不写入频道和节目缓存。
- 频道与 EPG 仅由刷新任务统一更新；请求 EPG/TVOD 时遇到明确的会话拒绝，会重新登录一次后原请求重试一次。
- 认证状态改为纯运行态，不再写入或恢复 `auth_status`、`user_token`、Cookie 和登录时间；启动时会自动清除旧认证状态。
- `/status.json` 成为唯一状态接口，移除 `/api/auth/status`。
- 删除 `auth.enabled`、`cookie`、主动登录和会话重建等旧配置字段；旧字段会被忽略。

## 功能

- 自动发现 IPTV 系统下发的 EPG 和 TVOD 基础地址，并保存 EPG 地址供下次启动使用。
- 缓存频道、节目单、TimeShiftURL、FCC 和回看日志。
- 输出 DIYP、酷9、rtp2httpd M3U 与 XMLTV。
- 当前直播优先走频道的 TimeShiftURL；历史节目按节目单调用 TVOD。
- 同名频道在酷9和 rtp2httpd 中保留首个名称，后续项显示为 `频道名 [频道号]`。
- rtp2httpd M3U 在频道提供 FCC 时输出 `?fcc=服务器:端口`。

## 部署

```bash
cp config/config.example.json config/config.json
nano config/config.json
docker compose up -d --build
```

使用预构建镜像包：

```bash
docker load -i hubei-telecom-iptv-gateway-go-1.0.4.tar
docker compose up -d
```

## 必填配置

编辑 `config/config.json` 中以下内容：

```jsonc
{
  "public_base_url": "http://192.168.10.12:8899",
  "rtsp_public_base_url": "rtsp://192.168.10.12:8555",
  "auth": {
    "user_id": "IPTV账号",
    "password": "IPTV密码",
    "stbid": "机顶盒STBID",
    "mac": "00:11:22:33:44:55",
    "auth_ip": "IPTV业务IP",
    "platform_base": "http://121.60.255.6:8080"
  }
}
```

`auth_ip` 一般填写 IPTV 业务网络中的 IP，常见为 `10.x.x.x`。`platform_base` 是湖北电信 Portal 地址，通常不需要修改。

`auth_session_ttl_seconds` 默认 `3600` 秒。超过该时间，状态接口会显示会话已过期；下一次需要认证的 EPG 或 TVOD 请求会自动重新登录。

## 常用地址

```text
http://网关IP:8899/
http://网关IP:8899/status.json
http://网关IP:8899/api/channels
http://网关IP:8899/ku9.m3u
http://网关IP:8899/rtp2httpd_catchup.m3u
http://网关IP:8899/diyp/live.txt
http://网关IP:8899/epg.xml
http://网关IP:8899/refresh?force=1
http://网关IP:8899/catchup_debug
http://网关IP:8899/catchup_log
```

`/refresh?force=1` 异步提交刷新：成功返回 `202 Accepted`；已有刷新任务时返回 `409 Conflict`。定时刷新碰到已有任务会在 300 秒后重试。

## 升级到 1.0.4

替换镜像并更新 `docker-compose.yml` 后重启即可。认证状态缓存会由程序自动删除，频道、节目单、TimeShiftURL 和 FCC 缓存可以保留。请从新的示例配置重新核对配置文件，移除已废弃字段。

## 说明

本项目仅用于个人学习和家庭局域网使用。请遵守当地法律、运营商业务和账号使用规则。
