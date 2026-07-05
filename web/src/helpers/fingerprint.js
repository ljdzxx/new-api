/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

const hashText = async (value) => {
  const text = String(value || '');
  if (!window.crypto?.subtle) {
    let hash = 0;
    for (let i = 0; i < text.length; i += 1) {
      hash = (hash << 5) - hash + text.charCodeAt(i);
      hash |= 0;
    }
    return Math.abs(hash).toString(16);
  }
  const data = new TextEncoder().encode(text);
  const digest = await window.crypto.subtle.digest('SHA-256', data);
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
};

const getCanvasValue = () => {
  const canvas = document.createElement('canvas');
  canvas.width = 320;
  canvas.height = 80;
  const ctx = canvas.getContext('2d');
  if (!ctx) return '';
  ctx.textBaseline = 'top';
  ctx.font = '16px Arial';
  ctx.fillStyle = '#f60';
  ctx.fillRect(4, 4, 120, 24);
  ctx.fillStyle = '#069';
  ctx.fillText('new-api invite risk 1.0', 8, 10);
  ctx.fillStyle = 'rgba(102, 204, 0, 0.7)';
  ctx.fillText('invite reward risk control', 12, 36);
  return canvas.toDataURL();
};

const getWebGLValue = () => {
  const canvas = document.createElement('canvas');
  const gl =
    canvas.getContext('webgl') || canvas.getContext('experimental-webgl');
  if (!gl) return '';
  const debugInfo = gl.getExtension('WEBGL_debug_renderer_info');
  const vendor = debugInfo
    ? gl.getParameter(debugInfo.UNMASKED_VENDOR_WEBGL)
    : gl.getParameter(gl.VENDOR);
  const renderer = debugInfo
    ? gl.getParameter(debugInfo.UNMASKED_RENDERER_WEBGL)
    : gl.getParameter(gl.RENDERER);
  return [
    vendor,
    renderer,
    gl.getParameter(gl.VERSION),
    gl.getParameter(gl.SHADING_LANGUAGE_VERSION),
  ].join('|');
};

const getAudioValue = async () => {
  const AudioContext = window.OfflineAudioContext;
  if (!AudioContext) return '';
  try {
    const context = new AudioContext(1, 44100, 44100);
    const oscillator = context.createOscillator();
    const compressor = context.createDynamicsCompressor();
    oscillator.type = 'triangle';
    oscillator.frequency.value = 10000;
    compressor.threshold.value = -50;
    compressor.knee.value = 40;
    compressor.ratio.value = 12;
    compressor.attack.value = 0;
    compressor.release.value = 0.25;
    oscillator.connect(compressor);
    compressor.connect(context.destination);
    oscillator.start(0);
    const buffer = await context.startRendering();
    const data = buffer.getChannelData(0).slice(4500, 5000);
    return Array.from(data)
      .map((v) => v.toFixed(6))
      .join(',');
  } catch (err) {
    return '';
  }
};

const getFontsValue = () => {
  const baseFonts = ['monospace', 'sans-serif', 'serif'];
  const testFonts = [
    'Arial',
    'Calibri',
    'Cambria',
    'Consolas',
    'Courier New',
    'Georgia',
    'Helvetica',
    'Microsoft YaHei',
    'PingFang SC',
    'Times New Roman',
    'Verdana',
  ];
  const testString = 'mmmmmmmmmmlli';
  const testSize = '72px';
  const span = document.createElement('span');
  span.style.position = 'absolute';
  span.style.left = '-9999px';
  span.style.fontSize = testSize;
  span.innerHTML = testString;
  document.body.appendChild(span);
  const baseDimensions = {};
  baseFonts.forEach((font) => {
    span.style.fontFamily = font;
    baseDimensions[font] = `${span.offsetWidth}x${span.offsetHeight}`;
  });
  const available = [];
  testFonts.forEach((font) => {
    const detected = baseFonts.some((base) => {
      span.style.fontFamily = `'${font}',${base}`;
      return (
        `${span.offsetWidth}x${span.offsetHeight}` !== baseDimensions[base]
      );
    });
    if (detected) available.push(font);
  });
  document.body.removeChild(span);
  return available.join('|');
};

export const generateInviteFingerprint = async () => {
  try {
    const canvas = await hashText(getCanvasValue());
    const webgl = await hashText(getWebGLValue());
    const audio = await hashText(await getAudioValue());
    const fonts = await hashText(getFontsValue());
    const ua = await hashText(
      [
        navigator.userAgent,
        navigator.userAgentData?.platform,
        navigator.userAgentData?.mobile,
      ].join('|'),
    );
    const locale = await hashText(
      [
        Intl.DateTimeFormat().resolvedOptions().timeZone,
        navigator.language,
        (navigator.languages || []).join(','),
      ].join('|'),
    );
    const screenHash = await hashText(
      [
        window.screen?.width,
        window.screen?.height,
        window.screen?.availWidth,
        window.screen?.availHeight,
        window.devicePixelRatio,
        window.screen?.colorDepth,
      ].join('|'),
    );
    const hardware = await hashText(
      [
        navigator.platform,
        navigator.hardwareConcurrency,
        navigator.deviceMemory,
        navigator.maxTouchPoints,
      ].join('|'),
    );
    const fingerprint = await hashText(
      [canvas, webgl, audio, fonts, ua, locale, screenHash, hardware].join('|'),
    );
    return {
      fingerprint_hash: fingerprint,
      canvas_hash: canvas,
      webgl_hash: webgl,
      audio_hash: audio,
      fonts_hash: fonts,
      ua_hash: ua,
      locale_hash: locale,
      screen_hash: screenHash,
      hardware_hash: hardware,
      missing: !fingerprint,
    };
  } catch (err) {
    return { missing: true };
  }
};
