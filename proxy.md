# Plan: proxy.py 支持入口/出口 HTTP+SOCKS5 认证代理

## Context

当前 `proxy.py` 是一个本地 HTTP/HTTPS 抓包代理，入口只支持无认证的 HTTP 代理协议，出口（`--proxy`）也只支持无认证或 HTTP Basic 认证的 HTTP 代理。需要改造为：

- 入口：同时支持 HTTP 代理和 SOCKS5 代理，分别监听不同端口，支持用户名密码认证
- 出口：支持 `http://user:pass@host:port` 和 `socks5://user:pass@host:port` 两种格式

## 改动文件

- `proxy.py`（唯一文件）

## 实现方案

### 1. 命令行参数调整

- `--port` 改为 HTTP 代理监听端口（保持默认 8888）
- 新增 `--socks-port`：SOCKS5 代理监听端口（如 1080），不指定则不启动 SOCKS5 入口
- 新增 `--auth`：入口认证凭据，格式 `user:pass`，同时应用于 HTTP 和 SOCKS5 入口
- `--proxy` 参数扩展：支持 `socks5://user:pass@host:port` 格式

### 2. 入口 HTTP 代理认证（Proxy-Authorization）

在 `handle_client` 中，收到请求后：
- 如果配置了 `--auth`，检查 `Proxy-Authorization: Basic <base64>` 头
- CONNECT 请求同样检查
- 认证失败返回 `407 Proxy Authentication Required` + `Proxy-Authenticate: Basic realm="proxy"`

### 3. 入口 SOCKS5 代理服务

新增 `Socks5Server` 类，监听 `--socks-port`：
- 实现 SOCKS5 握手协议（RFC 1928）
- 支持 `0x02` 用户名/密码认证方法（RFC 1929）
- 支持 CONNECT 命令（`0x01`）
- 连接建立后，复用现有的 `tunnel_blind` 或 MITM 逻辑

SOCKS5 握手流程：
1. 客户端发送方法协商：`VER(0x05) NMETHODS METHODS...`
2. 服务端选择方法：有 auth 时选 `0x02`，无 auth 时选 `0x00`
3. 如果选 `0x02`，进行用户名/密码子协商
4. 客户端发送请求：`VER CMD RSV ATYP DST.ADDR DST.PORT`
5. 服务端连接目标后回复成功

### 4. 出口 SOCKS5 代理支持

修改 `UpstreamProxy` 类：
- 解析 `socks5://` scheme
- SOCKS5 出口连接使用 PySocks（`import socks`）
- `connect_forward()` 和 `connect_tunnel()` 根据 scheme 分别走 HTTP 或 SOCKS5 路径
- SOCKS5 出口时：用 `socks.socksocket` 创建连接，设置代理类型、认证信息，然后 `connect((host, port))`

### 5. 代码结构

```
UpstreamProxy
  ├── scheme: "http" | "socks5"
  ├── host, port, username, password
  ├── connect_forward() → 对 HTTP 出口走原逻辑；对 SOCKS5 用 PySocks
  └── connect_tunnel() → 对 HTTP 出口走 CONNECT；对 SOCKS5 直接 connect 目标

ProxyServer（HTTP 入口，改动小）
  └── handle_client() 增加 407 认证检查

Socks5Server（新增，SOCKS5 入口）
  ├── serve_forever()
  ├── handle_client() → SOCKS5 握手 + 认证 + CONNECT
  └── 连接建立后复用 ProxyServer 的 tunnel_blind / MITM 逻辑

main()
  ├── 启动 HTTP ProxyServer 线程
  ├── 如果指定 --socks-port，启动 Socks5Server 线程
  └── 两个 server 共享 logger, cert_store, upstream_proxy, stop_event
```

### 6. 依赖

- 新增 `import socks`（PySocks 库，`pip install pysocks`）
- 仅用于出口 SOCKS5 代理连接

## 验证

1. 启动测试：`python proxy.py --port 8888 --socks-port 1080 --auth testuser:testpass --proxy socks5://user:pass@127.0.0.1:7890`
2. HTTP 入口认证测试：`curl -x http://testuser:testpass@127.0.0.1:8888 http://httpbin.org/ip`
3. SOCKS5 入口认证测试：`curl -x socks5://testuser:testpass@127.0.0.1:1080 http://httpbin.org/ip`
4. 无认证应被拒绝：`curl -x http://127.0.0.1:8888 http://httpbin.org/ip` → 407
5. 出口 SOCKS5 代理：确认流量经过指定的 SOCKS5 出口
