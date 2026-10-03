const maximumImageBytes = 32 * 1024 * 1024;

async function authorized(request, secret) {
  if (typeof secret !== 'string' || secret.length < 32) return false;
  const encoder = new TextEncoder();
  const supplied = request.headers.get('Authorization') || '';
  const expected = `Bearer ${secret}`;
  const hashes = await Promise.all([supplied, expected].map((value) =>
    crypto.subtle.digest('SHA-256', encoder.encode(value)),
  ));
  const suppliedHash = new Uint8Array(hashes[0]);
  const expectedHash = new Uint8Array(hashes[1]);
  let difference = 0;
  for (let index = 0; index < suppliedHash.length; index++) {
    difference |= suppliedHash[index] ^ expectedHash[index];
  }
  return difference === 0;
}

function validateSource(value, allowedHosts) {
  if (typeof value !== 'string') throw new Error('Invalid source URL');
  const source = new URL(value);
  const hostname = source.hostname.toLowerCase().replace(/\.$/, '');
  if (!['http:', 'https:'].includes(source.protocol) || source.username || source.password ||
      source.port ||
      !hostname.includes('.') || hostname.length > 253 ||
      !hostname.split('.').every((label) => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label)) ||
      /^\d+(?:\.\d+){3}$/.test(hostname) ||
      /\.(?:localhost|local|internal|lan|home|test|invalid|example)$/.test(hostname) ||
      (allowedHosts.size > 0 && !allowedHosts.has(hostname))) {
    throw new Error('Source URL is not allowed');
  }
  return source;
}

function publicIPv4(value) {
  if (!/^\d+(?:\.\d+){3}$/.test(value)) return false;
  const parts = value.split('.').map(Number);
  if (parts.some((part) => part < 0 || part > 255)) return false;
  const [first, second, third] = parts;
  return !(first === 0 || first === 10 || first === 127 || first >= 224 ||
    (first === 100 && second >= 64 && second <= 127) ||
    (first === 169 && second === 254) ||
    (first === 172 && second >= 16 && second <= 31) ||
    (first === 192 && (second === 168 ||
      (second === 0 && (third === 0 || third === 2)) || (second === 88 && third === 99))) ||
    (first === 198 && (second === 18 || second === 19 || (second === 51 && third === 100))) ||
    (first === 203 && second === 0 && third === 113));
}

function publicIPv6(value) {
  if (typeof value !== 'string' || !/^[a-f0-9:]+$/i.test(value)) return false;
  const halves = value.toLowerCase().split('::');
  if (halves.length > 2) return false;
  const left = halves[0] ? halves[0].split(':') : [];
  const right = halves.length === 2 && halves[1] ? halves[1].split(':') : [];
  const missing = 8 - left.length - right.length;
  if ((halves.length === 1 && missing !== 0) || (halves.length === 2 && missing < 1) ||
      [...left, ...right].some((part) => !/^[a-f0-9]{1,4}$/.test(part))) return false;
  const groups = [...left, ...Array(missing).fill('0'), ...right].map((part) => Number.parseInt(part, 16));
  const [first, second] = groups;
  return first >= 0x2000 && first <= 0x3fff && first !== 0x2002 &&
    !(first === 0x2001 && (second <= 0x01ff || second === 0x0db8)) &&
    !(first === 0x3fff && second <= 0x0fff);
}

async function validateSourceDNS(source, signal) {
  const hostname = source.hostname.toLowerCase().replace(/\.$/, '');
  const results = await Promise.all(['A', 'AAAA'].map(async (type) => {
    const resolver = new URL('https://cloudflare-dns.com/dns-query');
    resolver.searchParams.set('name', hostname);
    resolver.searchParams.set('type', type);
    const response = await fetch(resolver.toString(), {
      headers: { Accept: 'application/dns-json' }, redirect: 'error', signal,
    });
    if (!response.ok) throw new Error('Source DNS lookup failed');
    const result = await response.json();
    if (result.Status !== 0 || result.TC === true) throw new Error('Source DNS lookup failed');
    const answers = result.Answer ?? [];
    if (!Array.isArray(answers)) throw new Error('Invalid source DNS response');
    return answers.filter((answer) => answer.type === 1 || answer.type === 28);
  }));
  const addresses = results.flat();
  if (!addresses.length || addresses.some((answer) =>
    answer.type === 1 ? typeof answer.data !== 'string' || !publicIPv4(answer.data) : !publicIPv6(answer.data))) {
    throw new Error('Source DNS does not resolve exclusively to public addresses');
  }
}

async function downloadImage(source, allowedHosts, signal, limit) {
  let current = validateSource(source, allowedHosts);
  let response;
  for (let redirects = 0; redirects <= 5; redirects++) {
    await validateSourceDNS(current, signal);
    response = await fetch(current.toString(), { redirect: 'manual', signal });
    if (![301, 302, 303, 307, 308].includes(response.status)) break;
    const location = response.headers.get('Location');
    await response.body?.cancel();
    if (!location || redirects === 5) throw new Error('Invalid source redirect');
    current = validateSource(new URL(location, current).toString(), allowedHosts);
  }
  if (!response.ok || !response.body) {
    await response.body?.cancel();
    throw new Error('Source download failed');
  }
  if (Number(response.headers.get('Content-Length')) > limit) {
    await response.body.cancel();
    throw new Error('Image is too large');
  }
  const reader = response.body.getReader();
  const chunks = [];
  let size = 0;
  try {
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > limit) throw new Error('Image is too large');
      chunks.push(value);
    }
  } catch (error) {
    await reader.cancel().catch(() => {});
    throw error;
  }
  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  const contentType = imageContentType(bytes);
  if (!contentType) throw new Error('Source is not a supported image');
  return { bytes, contentType };
}

function imageContentType(bytes) {
  const startsWith = (signature) => bytes.length >= signature.length &&
    signature.every((value, index) => bytes[index] === value);
  if (startsWith([137, 80, 78, 71, 13, 10, 26, 10])) return 'image/png';
  if (startsWith([255, 216, 255])) return 'image/jpeg';
  const text = new TextDecoder('ascii').decode(bytes.subarray(0, 12));
  if (text.startsWith('GIF87a') || text.startsWith('GIF89a')) return 'image/gif';
  if (text.startsWith('RIFF') && text.slice(8, 12) === 'WEBP') return 'image/webp';
  return null;
}

export default {
  async fetch(request, env) {
    if (new URL(request.url).pathname !== '/import') return new Response('Not found', { status: 404 });
    if (request.method !== 'POST') return new Response('Method not allowed', { status: 405 });
    if (!await authorized(request, env.IMPORT_SECRET)) return new Response('Unauthorized', { status: 401 });
    const allowedHosts = new Set(String(env.ALLOWED_SOURCE_HOSTS || '').split(',')
      .map((host) => host.trim().toLowerCase().replace(/\.$/, '')).filter(Boolean));
    if (!env.IMAGES || !env.R2_BUCKET_NAME) {
      return new Response('Worker is not configured', { status: 503 });
    }
    const requestReader = request.body?.getReader();
    if (!requestReader) return new Response('Invalid request', { status: 400 });
    let requestText = '';
    let requestSize = 0;
    const decoder = new TextDecoder();
    try {
      while (true) {
        const { value, done } = await requestReader.read();
        if (done) break;
        requestSize += value.byteLength;
        if (requestSize > 16 * 1024) {
          await requestReader.cancel();
          return new Response('Request is too large', { status: 413 });
        }
        requestText += decoder.decode(value, { stream: true });
      }
      requestText += decoder.decode();
      const { source_url: sourceUrl, object_key: objectKey, bucket } = JSON.parse(requestText);
      const prefix = String(env.OBJECT_PREFIX || 'generated-images/').replace(/^\/+|\/+$/g, '') + '/';
      if (bucket !== env.R2_BUCKET_NAME || typeof objectKey !== 'string' ||
          !objectKey.startsWith(prefix) || objectKey.length > 1024 ||
          !/^[a-zA-Z0-9_./-]+$/.test(objectKey) ||
          objectKey.split('/').some((part) => !part || part === '.' || part === '..')) {
        return new Response('Invalid object key or bucket', { status: 400 });
      }
      validateSource(sourceUrl, allowedHosts);
      const configuredLimit = Number(env.MAX_IMAGE_BYTES || maximumImageBytes);
      const limit = Number.isSafeInteger(configuredLimit) && configuredLimit > 0
        ? Math.min(configuredLimit, maximumImageBytes) : maximumImageBytes;
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 120000);
      try {
        const { bytes, contentType } = await downloadImage(sourceUrl, allowedHosts, controller.signal, limit);
        await env.IMAGES.put(objectKey, bytes, { httpMetadata: { contentType } });
        return Response.json({ object_key: objectKey }, { headers: { 'Cache-Control': 'no-store' } });
      } finally {
        clearTimeout(timeout);
      }
    } catch {
      return new Response('Image import failed', { status: 502 });
    }
  },
};
