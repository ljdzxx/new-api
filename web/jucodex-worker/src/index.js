const CANONICAL_HOST = "jucodex.com"

export default {
  async fetch(request, env, ctx) {
    const incomingUrl = new URL(request.url)
    const pathname = safeDecodePath(incomingUrl.pathname)

    if (incomingUrl.hostname === "www." + CANONICAL_HOST) {
      incomingUrl.hostname = CANONICAL_HOST
      return Response.redirect(incomingUrl.toString(), 301)
    }

    if (shouldProxyToBackend(pathname)) {
      return proxyToBackend(request, env)
    }

    return serveStatic(request, env)
  },
}

// ---------- 静态文件（R2 自定义域名） ----------
async function serveStatic(request, env) {
  const staticOrigin = getStaticOrigin(env)
  if (!staticOrigin) {
    return new Response("Missing STATIC_ORIGIN", { status: 500 })
  }

  const pathname = safeDecodePath(new URL(request.url).pathname)

  const primaryResp = await fetchFromStaticOrigin(request, staticOrigin, pathname)
  if (primaryResp.status !== 404 || !shouldSpaFallback(pathname)) {
    return primaryResp
  }

  // SPA fallback：非静态资源路径（无扩展名）回退到 index.html
  return fetchFromStaticOrigin(request, staticOrigin, "/index.html")
}

async function fetchFromStaticOrigin(request, staticOrigin, pathname) {
  const incomingUrl = new URL(request.url)
  const target = new URL(staticOrigin)
  target.pathname = pathname
  target.search = incomingUrl.search

  const headers = new Headers(request.headers)
  headers.set("Host", target.host)

  const init = {
    method: request.method,
    headers,
    redirect: "manual",
  }

  if (request.method !== "GET" && request.method !== "HEAD") {
    init.body = request.body
    init.duplex = "half"
  }

  const upstreamReq = new Request(target.toString(), init)
  return fetch(upstreamReq)
}

function getStaticOrigin(env) {
  const raw = (env.STATIC_ORIGIN || "").trim()
  if (!raw) return ""
  if (/^https?:\/\//i.test(raw)) return raw.replace(/\/+$/, "")
  return `https://${raw}`.replace(/\/+$/, "")
}

function shouldSpaFallback(pathname) {
  if (pathname === "/" || pathname === "") return true
  const last = pathname.split("/").pop() || ""
  return !last.includes(".")
}

// ---------- 后端代理 ----------
async function proxyToBackend(request, env) {
  const incoming = new URL(request.url)

  const upstream = new URL(request.url)
  upstream.hostname = env.BACKEND_HOST
  upstream.protocol = env.BACKEND_PROTO || "https:"
  upstream.port = env.BACKEND_PORT || ""

  const headers = new Headers(request.headers)
  headers.set("Host", env.BACKEND_HOST)
  headers.set("X-Forwarded-Host", incoming.host)
  headers.set("X-Forwarded-Proto", incoming.protocol.replace(":", ""))

  const init = {
    method: request.method,
    headers,
    redirect: "manual",
  }

  if (request.method !== "GET" && request.method !== "HEAD") {
    init.body = request.body
    init.duplex = "half"
  }

  const resp = await fetch(new Request(upstream.toString(), init))
  const outHeaders = new Headers(resp.headers)

  // 动态接口默认不缓存
  outHeaders.set("Cache-Control", "no-store, no-cache, must-revalidate")

  return new Response(resp.body, {
    status: resp.status,
    statusText: resp.statusText,
    headers: outHeaders,
  })
}

function shouldProxyToBackend(pathname) {
  const prefixes = ["/api/", "/v1/", "/v1beta/", "/pg/", "/mj/", "/suno/"]

  if (
    pathname === "/api" ||
    pathname === "/v1" ||
    pathname === "/v1beta" ||
    pathname === "/pg" ||
    pathname === "/mj" ||
    pathname === "/suno"
  ) {
    return true
  }

  if (pathname === "/v1/realtime" || pathname.startsWith("/v1/realtime/")) {
    return true
  }

  return prefixes.some((p) => pathname.startsWith(p))
}

function safeDecodePath(pathname) {
  try {
    return decodeURIComponent(pathname)
  } catch {
    return pathname
  }
}