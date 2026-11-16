// Zoom de l'interface. L'UI a été dessinée pour ~2000 px de large utiles
// (Mac) : sur un écran plus étroit (ex. PC 150 % ≈ 1700 px), tout paraît
// trop gros. Zoom auto selon l'écran, réglable (Ctrl + / Ctrl − / Ctrl 0),
// mémorisé localement.

const KEY = 'go-post-tools-ui-zoom';
const DESIGN_WIDTH = 2000;

const round05 = (z) => Math.round(z * 20) / 20;
const clamp = (z, min, max) => Math.min(max, Math.max(min, z));

// Zoom automatique pour une largeur d'écran (px CSS). Jamais au-dessus de 100 %.
export function zoomForWidth(width) {
  if (!width || width <= 0) return 1;
  return clamp(round05(width / DESIGN_WIDTH), 0.7, 1);
}

function savedZoom() {
  try {
    const v = parseFloat(localStorage.getItem(KEY));
    return v >= 0.5 && v <= 1.5 ? v : null;
  } catch {
    return null;
  }
}

function applyZoom(z) {
  const root = document.documentElement;
  root.style.zoom = String(z);
  root.style.setProperty('--ui-zoom', String(z));
  window.dispatchEvent(new CustomEvent('ui-zoom-change', { detail: z }));
}

export function currentZoom() {
  return parseFloat(document.documentElement.style.zoom) || 1;
}

export function setZoom(z) {
  const v = clamp(round05(z), 0.6, 1.4);
  applyZoom(v);
  try { localStorage.setItem(KEY, String(v)); } catch { /* stockage indisponible */ }
  return v;
}

export function resetZoom() {
  try { localStorage.removeItem(KEY); } catch { /* stockage indisponible */ }
  const v = zoomForWidth(screen.availWidth || window.innerWidth);
  applyZoom(v);
  return v;
}

export function initZoom() {
  const saved = savedZoom();
  applyZoom(saved !== null ? saved : zoomForWidth(screen.availWidth || window.innerWidth));
  window.addEventListener('keydown', (e) => {
    if (!(e.ctrlKey || e.metaKey) || e.altKey) return;
    if (e.key === '=' || e.key === '+') { e.preventDefault(); setZoom(currentZoom() + 0.05); }
    else if (e.key === '-') { e.preventDefault(); setZoom(currentZoom() - 0.05); }
    else if (e.key === '0') { e.preventDefault(); resetZoom(); }
  });
}

// Pastille « 85 % » affichée brièvement quand le zoom change au clavier.
let toastTimer = null;
function showZoomToast(z) {
  let el = document.getElementById('ui-zoom-toast');
  if (!el) {
    el = document.createElement('div');
    el.id = 'ui-zoom-toast';
    el.style.cssText = 'position:fixed;top:14px;left:50%;transform:translateX(-50%);z-index:99999;padding:6px 14px;border-radius:999px;background:rgba(20,20,24,0.92);color:#fff;font:600 13px system-ui,sans-serif;box-shadow:0 6px 20px rgba(0,0,0,0.45);pointer-events:none;transition:opacity 200ms;';
    document.body.appendChild(el);
  }
  el.textContent = 'Zoom ' + Math.round(z * 100) + ' %';
  el.style.opacity = '1';
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.style.opacity = '0'; }, 1000);
}
window.addEventListener('ui-zoom-change', (e) => { if (document.body) showZoomToast(e.detail); });