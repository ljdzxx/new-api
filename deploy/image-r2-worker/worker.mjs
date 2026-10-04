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

function importFailure(diagnostic) {
  const reasons = {
    request_parse: 'invalid_request_json',
    source_download: diagnostic.upstreamStatus ? 'source_http_error' : 'source_fetch_failed',
    image_read: 'image_read_failed',
    r2_upload: 'r2_put_failed',
  };
  const details = {
    error: 'image_import_failed',
    stage: diagnostic.stage,
    reason: reasons[diagnostic.stage],
  };
  if (diagnostic.upstreamStatus) details.upstream_status = diagnostic.upstreamStatus;
  console.error('[image r2] import failed', details);
  return Response.json(details, { status: 502, headers: { 'Cache-Control': 'no-store' } });
}

export default {
  async fetch(request, env) {
    if (new URL(request.url).pathname !== '/import') return new Response('Not found', { status: 404 });
    if (request.method !== 'POST') return new Response('Method not allowed', { status: 405 });
    if (!await authorized(request, env.IMPORT_SECRET)) return new Response('Unauthorized', { status: 401 });
    if (!env.IMAGES || !env.R2_BUCKET_NAME) {
      return new Response('Worker is not configured', { status: 503 });
    }
    const diagnostic = { stage: 'request_parse' };
    try {
      const { source_url: sourceUrl, object_key: objectKey, bucket } = await request.json();
      if (typeof sourceUrl !== 'string' || !sourceUrl.trim() ||
          typeof objectKey !== 'string' || !objectKey.trim() || bucket !== env.R2_BUCKET_NAME) {
        return new Response('Invalid request', { status: 400 });
      }
      diagnostic.stage = 'source_download';
      const response = await fetch(sourceUrl, { redirect: 'follow' });
      if (!response.ok) {
        diagnostic.upstreamStatus = response.status;
        await response.body?.cancel();
        return importFailure(diagnostic);
      }
      diagnostic.stage = 'image_read';
      const bytes = await response.arrayBuffer();
      const contentType = response.headers.get('Content-Type') || 'application/octet-stream';
      diagnostic.stage = 'r2_upload';
      await env.IMAGES.put(objectKey, bytes, { httpMetadata: { contentType } });
      return Response.json({ object_key: objectKey }, { headers: { 'Cache-Control': 'no-store' } });
    } catch {
      return importFailure(diagnostic);
    }
  },
};
