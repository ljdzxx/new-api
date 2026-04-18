const CANONICAL_HOST = "jucodex.com"

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url)
    const pathname = decodeURIComponent(url.pathname)

    // 统一域名，可选
    if (url.hostname === "www.jucodex.com") {
      url.hostname = CANONICAL_HOST
      return Response.redirect(url.toString(), 301)
    }

    // API / WS 走后端
    if (shouldProxyToBackend(pathname)) {
      return proxyToBackend(request, env)
    }

    // 其余走 R2
    return serveFromR2(request, env)
  },
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

  return prefixes.some((prefix) => pathname.startsWith(prefix))
}

async function proxyToBackend(request, env) {
  const incomingUrl = new URL(request.url)
  const targetUrl = new URL(request.url)

  targetUrl.hostname = env.BACKEND_HOST
  targetUrl.protocol = env.BACKEND_PROTO || "https:"
  targetUrl.port = env.BACKEND_PORT || ""

  const headers = new Headers(request.headers)
  headers.set("Host", env.BACKEND_HOST)
  headers.set("X-Forwarded-Host", incomingUrl.host)
  headers.set("X-Forwarded-Proto", incomingUrl.protocol.replace(":", ""))

  const resp = await fetch(new Request(targetUrl.toString(), {
    method: request.method,
    headers,
    body: request.body,
    redirect: "manual",
    duplex: "half",
  }))

  const newHeaders = new Headers(resp.headers)
  newHeaders.set("Cache-Control", "no-store")

  return new Response(resp.body, {
    status: resp.status,
    statusText: resp.statusText,
    headers: newHeaders,
  })
}

async function serveFromR2(request, env) {
  const url = new URL(request.url)
  const pathname = decodeURIComponent(url.pathname)

  if (request.method !== "GET" && request.method !== "HEAD") {
    return new Response("Method Not Allowed", { status: 405 })
  }

  let key = pathnameToKey(pathname)
  let object = await env.ASSETS.get(key)

  if (!object && shouldFallbackToIndex(pathname)) {
    key = "index.html"
    object = await env.ASSETS.get("index.html")
  }

  if (!object) {
    return new Response("Not Found", { status: 404 })
  }

  const headers = new Headers()
  object.writeHttpMetadata(headers)
  headers.set("etag", object.httpEtag)

  if (key === "index.html") {
    headers.set("Cache-Control", "no-cache")
  } else if (isFingerprintAsset(key)) {
    headers.set("Cache-Control", "public, max-age=31536000, immutable")
  } else {
    headers.set("Cache-Control", "public, max-age=3600")
  }

  return new Response(object.body, {
    headers,
  })
}

function pathnameToKey(pathname) {
  if (pathname === "/" || pathname === "") return "index.html"

  let key = pathname.replace(/^\/+/, "")

  if (key.endsWith("/")) {
    key += "index.html"
  }

  return key
}

function shouldFallbackToIndex(pathname) {
  if (pathname === "/" || pathname === "") return true

  const last = pathname.split("/").pop() || ""
  return !last.includes(".")
}

function isFingerprintAsset(key) {
  return /[-.][a-f0-9]{6,}\./i.test(key)
}