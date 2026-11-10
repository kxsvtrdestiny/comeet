# LiveKit 配置与验证边界

优先使用LiveKit Cloud，获取步骤及本机.env路径见README。项目URL必须浏览器可达；Docker内部服务名不能填给浏览器。后端API_URL为https，浏览器WS_URL为wss；development模式只额外允许localhost/127.0.0.1/::1的http/ws。未配置不阻断文字会议。当前仅签发token，不在第四阶段调用管理API。

## 可选：Windows 本机自托管开发

此方案只用于同一台电脑的localhost验收，不代表公网生产部署。先在 https://github.com/livekit/livekit/releases/tag/v1.13.11 下载官方Windows amd64压缩包，核对该版本checksums.txt，解压使程序位于D:\LiveKit\livekit-server.exe。服务版本可根据官方说明更新，本次云端校验过的测试服务版本为1.13.11。

另开“终端 L” PowerShell，在本地生成仅自己保存的配置（不是默认公开密钥）：

```powershell
cd D:\LiveKit
$lkBytes = New-Object byte[] 40
$lkRng = [Security.Cryptography.RandomNumberGenerator]::Create()
$lkRng.GetBytes($lkBytes)
$lkRng.Dispose()
$lkSecret = [Convert]::ToBase64String($lkBytes)
$lkKey = "local_" + [Guid]::NewGuid().ToString("N")
$lkYaml = @"
port: 7880
bind_addresses: ["127.0.0.1"]
rtc:
  tcp_port: 7881
  udp_port: 7882
  node_ip: 127.0.0.1
  use_external_ip: false
keys:
  ${lkKey}: ${lkSecret}
"@
[IO.File]::WriteAllText("D:\LiveKit\livekit.yaml", $lkYaml)
$lkEnv = "LIVEKIT_API_URL=http://localhost:7880`r`nLIVEKIT_WS_URL=ws://localhost:7880`r`nLIVEKIT_API_KEY=$lkKey`r`nLIVEKIT_API_SECRET=$lkSecret`r`n"
[IO.File]::WriteAllText("D:\LiveKit\media-local.env", $lkEnv)
.\livekit-server.exe --config .\livekit.yaml
```

保持终端L开启。在本机记事本打开D:\LiveKit\media-local.env，把四行替换进comeet的server\.env中对应四行，保留JWT_SECRET等其他内容，保存、重启终端A后端。不要把这两个私有文件或内容发到聊天/仓库。使用APP_ENV=development。本机防火墙若询问权限，应按自己的设备/网络策略设置；不要开放公网开发密钥或关闭组织安全策略。回到Cloud时恢复Cloud四行并重启。

## 媒体权限与实现

Go复用已有jwt/v5按LiveKit公开HS256 token协议签名（官方protocol/auth/grants.go核对），无需引入整套服务端媒体传输依赖。iss为API Key，sub为服务端参会sessionId，room为内部会议UUID，显示名取服务端用户快照，metadata绑定userId/sessionId。TTL至多5分钟且不超过当前JWT/库内登录期限；禁止roomAdmin/roomCreate、data发布、修改元数据，发布源仅camera/microphone（无screen_share）。每次签发检查账号、sid、会议active、封禁、真实online连接及generation；预留/重连/离会不签发。

token在浏览器内存使用，不持久化，不打印日志。token有效期是加入/恢复的授权期限，不等于强制踢出已建立的LiveKit连接；即时服务端媒体移出和会控属于第五阶段。当前受控前端在业务终结/退出/离会时释放媒体，不声称已实现恶意客户端的管理API强制移出。

JS SDK锁定livekit-client2.23.0；720p约30fps、VP8、simulcast:true，h180/h360加主层，adaptiveStream:true、dynacast:true、autoSubscribe:true。依据该版本官方options.ts核对；配置写入并不等于已完成真实多层协商。使用用户提供的SDK本地轨道（独立gUM后包装LocalAudioTrack/LocalVideoTrack），避免SDK在设备断开后自动采集另一设备；切换仍调用SDK的switchActiveDevice。Go不重复转发SDP/ICE，也不用LiveKit数据通道替代业务聊天。

## 本次实际验证范围

后端权限/签名和设备预览自动检查结果见阶段验收。云端启动校验过官方SHA256的LiveKit1.13.11，真实信令已接受Go token。但当前Chromium管理策略WebRtcIPHandling=disable_non_proxied_udp，没有可用本地ICE候选，媒体未连通；没有修改/绕过策略。双账号音视频数据、实际VP8三层、远端订阅播放及媒体重连未标通过。

Windows Chrome/Edge、真实麦克风摄像头、扬声器听感、设备热插拔、跨网络/Cloud/TURN连通仍待人工验收。虚拟设备不等于真实设备，两个本机浏览器也不等于互联网跨网络测试。GitHub Pages不提供媒体测试环境。
