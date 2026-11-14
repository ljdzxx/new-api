# 登录cloudflare指令

npx wrangler login

# worker部署指令

npx wrangler deploy

# 本地回归测试

bun run test

测试覆盖 WebSocket 升级 Response 保留、请求头转发、HTTP/SSE 流与错误响应。
升级响应采用模拟对象；上线后仍需验证真实 WebSocket 握手和消息传输。
