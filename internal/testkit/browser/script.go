package browser

// pageScript is installed in every document before the document's own
// scripts. It is the page's half of the kit: what the browser adds is a way to
// run it.
//
// It keeps two things:
//
//   - the YPY requests in flight, from ypy:before:request until
//     ypy:finally:request, which YPY fires once for every request that was
//     admitted, whether it swapped, failed or was cancelled;
//   - the element the last lookup found, so the action that follows acts on
//     exactly that element.
//
// A Content-Security-Policy violation is reported through console.error with
// a fixed prefix, which reaches the test through the console events.
const pageScript = `(() => {
if (window.__hesape) return;
const norm = s => String(s == null ? '' : s).replace(/\s+/g, ' ').trim();
const requests = new Set();
const st = { target: null, control: null, leaving: 0 };
window.addEventListener("beforeunload", () => { st.leaving = Date.now(); });
const on = (name, fn) => document.addEventListener(name, fn, true);
const ctxOf = e => e && e.detail && e.detail.ctx;
on('ypy:before:request', e => { const c = ctxOf(e); if (c) requests.add(c); });
on('ypy:finally:request', e => { const c = ctxOf(e); if (c) requests.delete(c); });
document.addEventListener('securitypolicyviolation', e => {
  console.error('` + cspPrefix + `' + (e.blockedURI || 'inline') + ' (' + (e.effectiveDirective || e.violatedDirective) + ')');
}, true);

const busySelector = '.ypy-request,.ypy-swapping,.ypy-settling';
const ypyQuiet = () => requests.size === 0 && !document.querySelector(busySelector);
const finite = a => { try { return a.effect && a.effect.getComputedTiming().iterations !== Infinity; } catch (e) { return true; } };
const running = list => list.some(a => a.playState === 'running' && finite(a));
const animating = () => document.getAnimations ? running(document.getAnimations()) : false;

const visible = el => {
  if (!el || !el.isConnected) return false;
  const r = el.getBoundingClientRect();
  if (r.width <= 0 || r.height <= 0) return false;
  if (el.checkVisibility) return el.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true });
  const s = getComputedStyle(el);
  return s.display !== 'none' && s.visibility !== 'hidden' && parseFloat(s.opacity) > 0;
};

const implicitRole = el => {
  const t = el.tagName.toLowerCase();
  const type = (el.getAttribute('type') || '').toLowerCase();
  switch (t) {
    case 'a': case 'area': return el.hasAttribute('href') ? 'link' : null;
    case 'button': case 'summary': return 'button';
    case 'input':
      if (['button', 'submit', 'reset', 'image'].includes(type)) return 'button';
      if (type === 'checkbox') return el.hasAttribute('switch') ? 'switch' : 'checkbox';
      if (type === 'radio') return 'radio';
      if (type === 'range') return 'slider';
      if (type === 'number') return 'spinbutton';
      if (type === 'search') return el.hasAttribute('list') ? 'combobox' : 'searchbox';
      if (['', 'text', 'email', 'tel', 'url', 'password'].includes(type)) return el.hasAttribute('list') ? 'combobox' : 'textbox';
      return null;
    case 'select': return (el.multiple || el.size > 1) ? 'listbox' : 'combobox';
    case 'textarea': return 'textbox';
    case 'h1': case 'h2': case 'h3': case 'h4': case 'h5': case 'h6': return 'heading';
    case 'dialog': return 'dialog';
    case 'ul': case 'ol': case 'menu': return 'list';
    case 'li': return 'listitem';
    case 'nav': return 'navigation';
    case 'main': return 'main';
    case 'aside': return 'complementary';
    case 'header': return el.closest('article,aside,main,nav,section') ? null : 'banner';
    case 'footer': return el.closest('article,aside,main,nav,section') ? null : 'contentinfo';
    case 'form': return 'form';
    case 'table': return 'table';
    case 'tr': return 'row';
    case 'td': return 'cell';
    case 'th': return el.getAttribute('scope') === 'row' ? 'rowheader' : 'columnheader';
    case 'img': return el.getAttribute('alt') === '' ? 'presentation' : 'img';
    case 'option': return 'option';
    case 'section': return 'region';
    case 'article': return 'article';
    case 'fieldset': return 'group';
    case 'progress': return 'progressbar';
    case 'hr': return 'separator';
    case 'output': return 'status';
  }
  if (el.isContentEditable && !(el.parentElement && el.parentElement.isContentEditable)) return 'textbox';
  return null;
};
const roleOf = el => { const r = el.getAttribute('role'); return r ? r.trim().split(/\s+/)[0] : implicitRole(el); };

const hiddenForName = el => el.getAttribute('aria-hidden') === 'true' || el.hidden || getComputedStyle(el).display === 'none';
const textOf = (node, skip) => {
  if (node === skip) return '';
  if (node.nodeType === 3) return node.nodeValue;
  if (node.nodeType !== 1) return '';
  if (hiddenForName(node)) return '';
  const t = node.tagName.toLowerCase();
  if (t === 'script' || t === 'style' || t === 'template') return '';
  if (t === 'img') return ' ' + (node.getAttribute('alt') || '') + ' ';
  if (t === 'input' && ['button', 'submit', 'reset'].includes((node.type || '').toLowerCase())) return ' ' + node.value + ' ';
  let s = '';
  for (const c of node.childNodes) s += textOf(c, skip);
  return getComputedStyle(node).display.startsWith('inline') ? s : ' ' + s + ' ';
};
const byIds = ids => ids.split(/\s+/).map(id => document.getElementById(id)).filter(Boolean).map(e => textOf(e)).join(' ');
const isControl = el => ['input', 'select', 'textarea', 'button', 'meter', 'output', 'progress'].includes(el.tagName.toLowerCase());
const labelName = el => {
  const lb = el.getAttribute('aria-labelledby');
  if (lb) { const s = norm(byIds(lb)); if (s) return s; }
  const al = norm(el.getAttribute('aria-label'));
  if (al) return al;
  if (el.labels && el.labels.length) return norm(Array.from(el.labels).map(l => textOf(l, el)).join(' '));
  return '';
};
const contentRoles = new Set(['button', 'link', 'heading', 'cell', 'columnheader', 'rowheader', 'option', 'listitem', 'tab', 'menuitem', 'menuitemcheckbox', 'menuitemradio', 'checkbox', 'radio', 'switch', 'tooltip', 'treeitem', 'row', 'gridcell']);
const nameOf = el => {
  const t = el.tagName.toLowerCase();
  const label = labelName(el);
  if (label) return label;
  if (t === 'input') {
    const type = (el.type || '').toLowerCase();
    if (['button', 'submit', 'reset'].includes(type)) return norm(el.value || (type === 'submit' ? 'Submit' : type === 'reset' ? 'Reset' : ''));
    if (type === 'image') return norm(el.getAttribute('alt'));
  }
  if (isControl(el) && t !== 'button') { const ph = norm(el.getAttribute('placeholder')); if (ph) return ph; }
  if (t === 'img' || t === 'area') { const a = norm(el.getAttribute('alt')); if (a) return a; }
  if (t === 'fieldset') { const lg = el.querySelector(':scope > legend'); if (lg) return norm(textOf(lg)); }
  if (t === 'table') { const c = el.querySelector(':scope > caption'); if (c) return norm(textOf(c)); }
  const role = roleOf(el);
  if (role && contentRoles.has(role)) { const s = norm(textOf(el)); if (s) return s; }
  if (role === 'dialog' || role === 'alertdialog') { const h = el.querySelector('h1,h2,h3,h4,h5,h6,[role=heading]'); if (h) return norm(textOf(h)); }
  return norm(el.getAttribute('title'));
};
const sameName = (have, want) => have === want || have.replace(/\s*\*$/, '') === want;

const describeLoc = l => l.kind === 'role' ? 'role=' + l.a + (l.b ? ' name="' + l.b + '"' : '') : l.kind + '="' + l.a + '"';
const describe = el => {
  if (!el) return 'nothing';
  if (el.nodeType !== 1) return el.nodeName;
  let s = el.tagName.toLowerCase();
  if (el.id) s += '#' + el.id;
  if (typeof el.className === 'string' && el.className.trim()) s += '.' + el.className.trim().split(/\s+/).slice(0, 2).join('.');
  const role = roleOf(el); if (role) s += '[role=' + role + ']';
  const name = nameOf(el); if (name) s += ' "' + (name.length > 40 ? name.slice(0, 40) + '…' : name) + '"';
  return s;
};

const controlSelector = 'input:not([type=hidden]),select,textarea,[contenteditable=""],[contenteditable=true],[role=textbox],[role=combobox],[role=checkbox],[role=radio],[role=switch],[role=slider],[role=spinbutton],[role=searchbox],[role=listbox]';
const within = (root, sel) => Array.from(root.querySelectorAll(sel));
const resolve = l => {
  let els;
  switch (l.kind) {
    case 'role': els = within(document, '*').filter(el => roleOf(el) === l.a && (!l.b || sameName(nameOf(el), l.b))); break;
    case 'label': els = within(document, controlSelector).filter(el => sameName(labelName(el), l.a)); break;
    case 'css': try { els = within(document, l.a); } catch (e) { return { error: 'invalid selector ' + l.a + ': ' + e.message }; } break;
    default: return { error: 'unknown locator ' + l.kind };
  }
  return { els };
};

const frames = () => Promise.race([
  new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))),
  new Promise(r => setTimeout(r, 100)),
]);
const rectOf = el => { const r = el.getBoundingClientRect(); return [r.x, r.y, r.width, r.height].map(v => Math.round(v * 10) / 10); };
const disabled = el => el.disabled === true || !!el.closest('fieldset[disabled]') || el.getAttribute('aria-disabled') === 'true';
const pointOf = el => {
  const rects = el.getClientRects();
  const r = rects.length ? rects[0] : el.getBoundingClientRect();
  const x = Math.min(Math.max(r.x + r.width / 2, 0), innerWidth - 1);
  const y = Math.min(Math.max(r.y + r.height / 2, 0), innerHeight - 1);
  return { x, y };
};
const reaches = (el, hit) => !!hit && (el === hit || el.contains(hit) || hit.control === el || (hit.closest && hit.closest('label') && hit.closest('label').control === el));

window.__hesape = {
  state() {
    return { ready: document.readyState, ypy: ypyQuiet(), animating: animating(), leaving: st.leaving ? Date.now() - st.leaving : -1 };
  },
  async find(l, action) {
    const res = resolve(l);
    if (res.error) return { status: 'none', detail: res.error };
    const els = res.els;
    const vis = els.filter(visible);
    if (action === 'hidden') return vis.length === 0 ? { status: 'ok' } : { status: 'visible', detail: describeLoc(l) + ' is visible: ' + describe(vis[0]) };
    if (els.length === 0) return { status: 'none', detail: 'no element matches ' + describeLoc(l) };
    // A styled checkbox or radio hides the native control and shows its
    // label; a person clicks the label, and so does the check.
    let control = vis.length === 1 ? vis[0] : null, el = control;
    if (action === 'check' && vis.length === 0 && els.length === 1 && els[0].labels) {
      const label = Array.from(els[0].labels).find(visible);
      if (label) { control = els[0]; el = label; }
    }
    if (!el && vis.length === 0) return { status: 'hidden', detail: els.length + ' element(s) match ' + describeLoc(l) + ' and none is visible: ' + els.slice(0, 3).map(describe).join(', ') };
    if (vis.length > 1) return { status: 'many', detail: vis.length + ' visible elements match ' + describeLoc(l) + ': ' + vis.slice(0, 5).map(describe).join(', ') };
    st.target = el; st.control = control;
    if (action === 'visible') return { status: 'ok', description: describe(el), text: norm(el.innerText || el.value || '') };
    if (disabled(control)) return { status: 'disabled', detail: describe(control) + ' is disabled' };
    if (action === 'fill' && (el.readOnly || (!isControl(el) && !el.isContentEditable))) return { status: 'readonly', detail: describe(el) + ' cannot be typed into' };
    el.scrollIntoView({ block: 'center', inline: 'center', behavior: 'instant' });
    const before = rectOf(el);
    await frames();
    const after = rectOf(el);
    if (before.join() !== after.join() || (el.getAnimations && running(el.getAnimations({ subtree: true })))) return { status: 'moving', detail: describe(el) + ' is still moving' };
    const p = pointOf(el);
    const hit = document.elementFromPoint(p.x, p.y);
    if (!reaches(el, hit)) return { status: 'covered', detail: describe(el) + ' is covered by ' + describe(hit) + ' at ' + Math.round(p.x) + ',' + Math.round(p.y) };
    return { status: 'ok', x: p.x, y: p.y, description: describe(el) };
  },
  focusTarget() {
    const el = st.target;
    if (!el || !el.isConnected) return { ok: false, detail: 'the element went away' };
    if (document.activeElement !== el) el.focus();
    const type = (el.type || '').toLowerCase();
    const native = ['date', 'datetime-local', 'month', 'week', 'time', 'color', 'range'].includes(type);
    if (!native) {
      if (el.isContentEditable) { const r = document.createRange(); r.selectNodeContents(el); const s = getSelection(); s.removeAllRanges(); s.addRange(r); }
      else if (el.select) { try { el.select(); } catch (e) {} }
    }
    return { ok: document.activeElement === el, native, detail: document.activeElement === el ? '' : 'focus went to ' + describe(document.activeElement) };
  },
  setTargetValue(v) {
    const el = st.target;
    const proto = Object.getPrototypeOf(el);
    const desc = Object.getOwnPropertyDescriptor(proto, 'value');
    if (desc && desc.set) desc.set.call(el, v); else el.value = v;
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
    return norm(el.value);
  },
  controlChecked() { const el = st.control; return !!(el && (el.checked || el.getAttribute('aria-checked') === 'true')); },
  text() { return norm(document.body ? document.body.innerText : ''); },
};
})();`

// cspPrefix starts the console error the page script writes for every
// Content-Security-Policy violation.
const cspPrefix = "content security policy blocked "
