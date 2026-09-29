// This is application code, not model output. CSP authorizes only this hash.
(() => {
  const root = document.documentElement;
  const canvas = document.body;
  const set = (element, name, value) => element.style.setProperty(name, value, 'important');
  set(root, 'overflow', 'hidden');
  set(root, 'height', '100%');
  set(root, 'width', '100%');
  set(canvas, 'position', 'absolute');
  set(canvas, 'box-sizing', 'border-box');
  set(canvas, 'width', '960px');
  set(canvas, 'min-width', '960px');
  set(canvas, 'max-width', 'none');
  set(canvas, 'height', 'auto');
  set(canvas, 'min-height', '640px');
  set(canvas, 'max-height', 'none');
  set(canvas, 'margin', '0');
  set(canvas, 'transform-origin', '0 0');
  set(canvas, 'overflow', 'hidden');
  let pending = false;
  const fit = () => {
    pending = false;
    const width = Math.max(960, canvas.scrollWidth, canvas.offsetWidth);
    const height = Math.max(640, canvas.scrollHeight, canvas.offsetHeight);
    const scale = Math.min(innerWidth / width, innerHeight / height);
    set(canvas, 'transform', `scale(${scale})`);
    set(canvas, 'left', `${Math.max(0, (innerWidth - width * scale) / 2)}px`);
    set(canvas, 'top', `${Math.max(0, (innerHeight - height * scale) / 2)}px`);
  };
  const schedule = () => {
    if (!pending) { pending = true; requestAnimationFrame(fit); }
  };
  new ResizeObserver(schedule).observe(canvas);
  addEventListener('resize', schedule);
  addEventListener('load', schedule);
  document.fonts?.ready.then(schedule);
  fit();
})();
