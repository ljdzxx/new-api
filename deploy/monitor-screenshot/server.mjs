import http from 'node:http';
import { chromium } from 'playwright';

const browser = await chromium.launch({
  headless: true,
  ...(process.env.CHROMIUM_EXECUTABLE_PATH
    ? { executablePath: process.env.CHROMIUM_EXECUTABLE_PATH }
    : {}),
});
let active = 0;
http.createServer(async (req, res) => {
  if (req.method !== 'POST' || req.url !== '/screenshot') { res.writeHead(404).end(); return; }
  if (active >= 2) { res.writeHead(429).end(); return; }
  active++;
  let context;
  try {
    const chunks = []; let size = 0;
    for await (const chunk of req) {
      size += chunk.length;
      if (size > 2 * 1024 * 1024) { res.writeHead(413).end(); return; }
      chunks.push(chunk);
    }
    const { html } = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    if (typeof html !== 'string' || html.length > 1024 * 1024) { res.writeHead(400).end(); return; }
    context = await browser.newContext({ javaScriptEnabled: false, serviceWorkers: 'block', viewport: { width: 960, height: 640 } });
    await context.route('**/*', (route) => route.abort());
    const page = await context.newPage();
    page.setDefaultTimeout(15000);
    await page.setContent(html, { waitUntil: 'load', timeout: 15000 });
    const png = await page.screenshot({ type: 'png', timeout: 15000 });
    res.writeHead(200, { 'Content-Type': 'image/png', 'Content-Length': png.length }).end(png);
  } catch (error) {
    console.error("Screenshot failed:", error);
    if (!res.headersSent) res.writeHead(500);
    res.end('Screenshot failed');
  } finally {
    try { await context?.close(); } finally { active--; }
  }
}).listen(3011, '0.0.0.0');
