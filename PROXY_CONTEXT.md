# proxy.py 改造进度记录

## 目标

将 `proxy.py` 改造为入口/出口同时支持 HTTP 和 SOCKS5 认证代理：

- **入口（监听）**：HTTP 代理 `--port`（默认 8888）+ SOCKS5 代理 `--socks-port`（单独端口），使用 `--auth user:pass` 认证
- **出口（上游）**：`--proxy` 参数支持 `http://user:pass@host:port` 和 `socks5://user:pass@host:port`

## 已完成的修改

### 1. 导入部分
- 新增 `import struct`（用于 SOCKS5 协议二进制打包）
- 新增条件导入 `import socks as pysocks`（PySocks 库，用于出口 SOCKS5），缺失时设为 `None`

### 2. UpstreamProxy 类完全重写
- 解析 `http://` 和 `socks5://` 两种 scheme
- 支持 URL 中嵌入用户名密码（`urllib.parse.urlsplit` + `unquote`）
- `socks5` scheme 默认端口 1080，`http` 默认端口 80
- `_socks5_socket(dest_host, dest_port)`：用 PySocks 创建 SOCKS5 代理连接
- `connect_forward()`：仅限 HTTP scheme，socks5 时抛异常
- `connect_tunnel(host, port)`：socks5 走 `_socks5_socket` 直连目标；http 走 CONNECT 隧道
- HTTP scheme 支持 `Proxy-Authorization: Basic` 头

### 3. handle_one_request 修复
- 将 `absolute_form` 判断改为 `can_absolute`：只有上游是 `http` scheme 且非 TLS 时才用 HTTP absolute-form 转发
- socks5 上游时，所有请求（包括 HTTP 明文）都走 `connect_tunnel` 隧道模式
- 避免了 socks5 上游调用 `connect_forward()` 导致的 RuntimeError

### 4. ProxyServer 构造函数
- 新增 `auth` 参数，存储为 `self.auth`（格式 `"user:pass"` 或 `None`）

### 5. HTTP 入口认证
- 新增 `_check_proxy_auth(headers)` 方法：解析 `Proxy-Authorization: Basic <base64>` 头，与 `self.auth` 比较
- `handle_client` 中，在路由到 CONNECT/普通请求前检查认证
- 认证失败返回 `HTTP/1.1 407 Proxy Authentication Required` + `Proxy-Authenticate: Basic realm="proxy"`
- CONNECT 和普通 HTTP 请求均受认证保护

### 6. handle_connect 重构
- 提取 `_relay_after_connect(client, request_id, host, port)` 方法
- 包含 MITM 判断、blind tunnel、TLS 包装和 HTTP 请求循环
- `handle_connect` 发送 200 后调用 `_relay_after_connect`
- `Socks5Server` 在 SOCKS5 握手后复用同一方法

### 7. SOCKS5 入口服务器（Socks5Server 类）
- 监听 `--socks-port` 端口
- 实现 RFC 1928 SOCKS5 握手：VER + 方法协商
- 支持 RFC 1929 用户名/密码认证（`0x02` 方法），无 auth 时用 `0x00`
- 支持 CONNECT 命令（`0x01`），解析 IPv4 / Domain / IPv6 地址
- 使用 `_recv_exact(sock, n)` 静态方法确保读取完整字节
- 连接建立后调用 `self.proxy._relay_after_connect()` 复用 ProxyServer 的 tunnel/MITM 逻辑
- 共享 ProxyServer 的 `stop_event`、`logger`、socket/thread 跟踪

### 8. install_shutdown_handlers 更新
- 函数签名改为 `*servers`，支持同时管理多个 server
- shutdown 时遍历所有 server 调用 `shutdown()`
- Windows 控制台处理也遍历所有 server 的 `join_threads`

### 9. main() 更新
- 新增 `--socks-port` 参数（SOCKS5 监听端口，不指定则不启动）
- 新增 `--auth` 参数（格式 `user:pass`，同时应用于 HTTP 和 SOCKS5 入口）
- `--proxy` help 文本更新，说明支持 `socks5://`
- 如果指定 `--socks-port`，在 daemon 线程中启动 `Socks5Server`
- `ProxyServer` 构造传入 `auth` 参数
- shutdown 时遍历所有 server

### 10. 语法验证
- `python -m py_compile proxy.py` 通过，无语法错误

## 状态：全部完成 ✓

## 依赖

- PySocks（`pip install pysocks`）：仅出口 SOCKS5 代理需要
- SOCKS5 入口是纯 Python 手写协议实现，不依赖第三方库

## 验证命令

```bash
# 启动测试（全功能）
python proxy.py --port 8888 --socks-port 1080 --auth testuser:testpass --proxy socks5://user:pass@127.0.0.1:7890

# HTTP 入口认证测试
curl -x http://testuser:testpass@127.0.0.1:8888 http://httpbin.org/ip

# SOCKS5 入口认证测试
curl -x socks5://testuser:testpass@127.0.0.1:1080 http://httpbin.org/ip

# 无认证应被拒绝（HTTP → 407）
curl -x http://127.0.0.1:8888 http://httpbin.org/ip

# 出口 SOCKS5 代理
python proxy.py --port 8888 --proxy socks5://127.0.0.1:7890
```
