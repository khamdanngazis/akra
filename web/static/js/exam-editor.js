// Editor soal dan stimulus (T128). Dimuat hanya di halaman editor.
// Teks kaya ditulis di elemen contenteditable lalu disalin ke <textarea>
// tersembunyi; server membersihkan dan merender ulang semuanya (rumus
// dengan KaTeX di server), jadi kode ini hanya untuk kenyamanan menulis.
(function () {
  'use strict';
  const form = document.querySelector('form[data-editor]');
  if (!form) return;

  const csrf = () => (document.querySelector('meta[name="csrf-token"]') || {}).content || '';
  const statusEl = document.querySelector('[data-save-status]');
  const setStatus = (t) => { if (statusEl) statusEl.textContent = t; };
  const ALLOWED = new Set(['P', 'BR', 'STRONG', 'B', 'EM', 'I', 'U', 'SUB', 'SUP', 'UL', 'OL', 'LI', 'TABLE', 'THEAD', 'TBODY', 'TR', 'TH', 'TD', 'IMG', 'SPAN', 'DIV']);

  // ---- Rumus ----
  function renderMath(span) {
    const tex = span.getAttribute('data-tex') || '';
    span.contentEditable = 'false';
    span.textContent = '';
    if (window.katex) {
      try {
        window.katex.render(tex, span, { throwOnError: true, displayMode: span.classList.contains('math-display'), trust: false });
        span.removeAttribute('data-error');
        return;
      } catch (e) {
        span.setAttribute('data-error', e.message);
      }
    }
    span.textContent = '\\(' + tex + '\\)';
  }
  const renderAllMath = (root) => root.querySelectorAll('span.math').forEach(renderMath);

  // serialize menyalin isi editor ke HTML sumber: rumus hanya data-tex,
  // gambar hanya data-asset dan alt.
  function serialize(area) {
    const clone = area.cloneNode(true);
    clone.querySelectorAll('span.math').forEach((s) => {
      const n = document.createElement('span');
      n.className = s.classList.contains('math-display') ? 'math math-display' : 'math';
      n.setAttribute('data-tex', s.getAttribute('data-tex') || '');
      s.replaceWith(n);
    });
    clone.querySelectorAll('img').forEach((img) => {
      img.removeAttribute('src');
      img.removeAttribute('style');
    });
    return clone.innerHTML;
  }

  // cleanPaste membuang atribut dan elemen di luar allowlist dari HTML
  // tempelan (Word, web). Server tetap membersihkan ulang.
  function cleanPaste(html) {
    const doc = new DOMParser().parseFromString(html, 'text/html');
    doc.querySelectorAll('script,style,meta,link,title,o\\:p,xml').forEach((n) => n.remove());
    const walk = (node) => {
      [...node.childNodes].forEach((ch) => {
        if (ch.nodeType === Node.COMMENT_NODE) { ch.remove(); return; }
        if (ch.nodeType !== Node.ELEMENT_NODE) return;
        walk(ch);
        if (!ALLOWED.has(ch.tagName) || (ch.tagName === 'IMG' && !ch.hasAttribute('data-asset'))) {
          if (ch.tagName === 'IMG') { ch.remove(); return; }
          ch.replaceWith(...ch.childNodes);
          return;
        }
        [...ch.attributes].forEach((a) => {
          const keep = ['dir', 'lang', 'colspan', 'rowspan', 'data-asset', 'alt', 'data-tex'].includes(a.name) ||
            (a.name === 'class' && /\bmath\b/.test(a.value));
          if (!keep) ch.removeAttribute(a.name);
        });
      });
    };
    walk(doc.body);
    return doc.body.innerHTML;
  }

  // ---- Gambar ----
  // shrink mengecilkan foto di browser (sisi terpanjang 1600 px, WebP).
  function shrink(file) {
    return new Promise((resolve) => {
      if (!/^image\/(png|jpeg|webp)$/.test(file.type)) { resolve(file); return; }
      const img = new Image();
      img.onload = () => {
        const s = Math.min(1, 1600 / Math.max(img.width, img.height));
        if (s === 1 && file.size < 400 * 1024) { URL.revokeObjectURL(img.src); resolve(file); return; }
        const c = document.createElement('canvas');
        c.width = Math.round(img.width * s); c.height = Math.round(img.height * s);
        c.getContext('2d').drawImage(img, 0, 0, c.width, c.height);
        URL.revokeObjectURL(img.src);
        c.toBlob((b) => resolve(b && b.size < file.size ? new File([b], file.name.replace(/\.\w+$/, '') + '.webp', { type: b.type }) : file), 'image/webp', 0.85);
      };
      img.onerror = () => resolve(file);
      img.src = URL.createObjectURL(file);
    });
  }

  async function uploadImage(file, area) {
    setStatus('Mengunggah gambar…');
    const fd = new FormData();
    fd.append('file', await shrink(file));
    try {
      const res = await fetch(form.dataset.upload, { method: 'POST', body: fd, headers: { 'X-CSRF-Token': csrf() }, credentials: 'same-origin' });
      const data = await res.json();
      if (!res.ok) { alert(data.error || 'Gambar gagal diunggah.'); setStatus(''); return; }
      const suggestion = file.name.replace(/\.\w+$/, '').replace(/[-_]+/g, ' ');
      const alt = window.prompt('Teks alternatif gambar (untuk pembaca layar), mis. "Segitiga siku-siku ABC":', suggestion);
      const img = document.createElement('img');
      img.setAttribute('data-asset', data.id);
      img.alt = (alt || suggestion).trim();
      img.src = data.url;
      insertNode(area, img);
      setStatus('Gambar disisipkan.');
    } catch (e) {
      alert('Gambar gagal diunggah. Periksa koneksi.');
      setStatus('');
    }
  }

  function insertNode(area, node) {
    area.focus();
    const sel = window.getSelection();
    if (sel.rangeCount && area.contains(sel.anchorNode)) {
      const r = sel.getRangeAt(0);
      r.deleteContents();
      r.insertNode(node);
      r.setStartAfter(node);
      sel.removeAllRanges();
      sel.addRange(r);
    } else {
      area.appendChild(node);
    }
    area.dispatchEvent(new Event('input', { bubbles: true }));
  }

  // ---- Dialog rumus ----
  const dialog = document.getElementById('math-dialog');
  const texInput = document.getElementById('math-tex');
  const preview = document.getElementById('math-preview');
  const errEl = document.getElementById('math-error');
  const displayBox = document.getElementById('math-display');
  let mathTarget = null; // { area, span? }

  function updatePreview() {
    if (!window.katex) return;
    try {
      window.katex.render(texInput.value || '\\square', preview, { throwOnError: true, displayMode: displayBox.checked, trust: false });
      errEl.hidden = true;
    } catch (e) {
      errEl.textContent = 'Rumus belum benar: ' + e.message.replace(/^KaTeX parse error: /, '');
      errEl.hidden = false;
    }
  }
  function openMath(area, span) {
    mathTarget = { area, span, range: saveRange(area) };
    texInput.value = span ? span.getAttribute('data-tex') : '';
    displayBox.checked = !!(span && span.classList.contains('math-display'));
    updatePreview();
    dialog.showModal();
    texInput.focus();
  }
  function saveRange(area) {
    const sel = window.getSelection();
    return sel.rangeCount && area.contains(sel.anchorNode) ? sel.getRangeAt(0).cloneRange() : null;
  }
  if (dialog) {
    texInput.addEventListener('input', updatePreview);
    displayBox.addEventListener('change', updatePreview);
    dialog.querySelectorAll('[data-tex]').forEach((b) => b.addEventListener('click', () => {
      const t = b.getAttribute('data-tex');
      const s = texInput.selectionStart, e = texInput.selectionEnd;
      texInput.value = texInput.value.slice(0, s) + t + texInput.value.slice(e);
      texInput.focus();
      texInput.selectionStart = texInput.selectionEnd = s + t.length;
      updatePreview();
    }));
    document.getElementById('math-insert').addEventListener('click', () => {
      const tex = texInput.value.trim();
      if (!tex || !errEl.hidden) { updatePreview(); return; }
      const { area, span, range } = mathTarget;
      const el = span || document.createElement('span');
      el.className = displayBox.checked ? 'math math-display' : 'math';
      el.setAttribute('data-tex', tex);
      renderMath(el);
      if (!span) {
        if (range) { const sel = window.getSelection(); sel.removeAllRanges(); sel.addRange(range); }
        insertNode(area, el);
      }
      area.dispatchEvent(new Event('input', { bubbles: true }));
      dialog.close();
    });
  }

  // ---- Editor per kolom ----
  let dirty = false;
  function setupRte(box) {
    const area = box.querySelector('.rte-area');
    const store = box.querySelector('textarea');
    const sync = () => { store.value = serialize(area); };
    area.querySelectorAll('img[data-asset]').forEach((img) => { if (!img.src) img.src = '/ujian/aset/' + img.getAttribute('data-asset'); });
    renderAllMath(area);
    sync();
    area.addEventListener('input', () => { sync(); dirty = true; });
    area.addEventListener('click', (e) => {
      const m = e.target.closest('span.math');
      if (m && area.contains(m)) openMath(area, m);
    });
    area.addEventListener('paste', (e) => {
      const files = [...(e.clipboardData.files || [])].filter((f) => f.type.startsWith('image/'));
      if (files.length) { e.preventDefault(); files.forEach((f) => uploadImage(f, area)); return; }
      const html = e.clipboardData.getData('text/html');
      if (html) {
        e.preventDefault();
        document.execCommand('insertHTML', false, cleanPaste(html));
        renderAllMath(area);
      }
    });
    area.addEventListener('dragover', (e) => e.preventDefault());
    area.addEventListener('drop', (e) => {
      const files = [...e.dataTransfer.files].filter((f) => f.type.startsWith('image/'));
      if (files.length) { e.preventDefault(); files.forEach((f) => uploadImage(f, area)); }
    });
    box.querySelectorAll('[data-cmd]').forEach((btn) => btn.addEventListener('mousedown', (e) => e.preventDefault()));
    box.querySelectorAll('[data-cmd]').forEach((btn) => btn.addEventListener('click', () => {
      const cmd = btn.dataset.cmd;
      area.focus();
      if (cmd === 'math') { openMath(area, null); return; }
      if (cmd === 'image') {
        const inp = document.createElement('input');
        inp.type = 'file'; inp.accept = 'image/png,image/jpeg,image/webp,image/gif';
        inp.onchange = () => inp.files[0] && uploadImage(inp.files[0], area);
        inp.click();
        return;
      }
      if (cmd === 'table') {
        document.execCommand('insertHTML', false, '<table><tbody><tr><td>&nbsp;</td><td>&nbsp;</td></tr><tr><td>&nbsp;</td><td>&nbsp;</td></tr></tbody></table><p><br></p>');
      } else if (cmd === 'dir') {
        const sel = window.getSelection();
        let block = sel.anchorNode && (sel.anchorNode.nodeType === 1 ? sel.anchorNode : sel.anchorNode.parentElement);
        block = block && block.closest('p,li,td,th');
        if (!block || !area.contains(block)) {
          document.execCommand('formatBlock', false, 'p');
          block = window.getSelection().anchorNode;
          block = block && (block.nodeType === 1 ? block : block.parentElement).closest('p');
        }
        if (block) {
          const rtl = block.getAttribute('dir') !== 'rtl';
          block.setAttribute('dir', rtl ? 'rtl' : 'ltr');
          if (rtl) block.setAttribute('lang', 'ar'); else block.removeAttribute('lang');
        }
      } else {
        document.execCommand(cmd, false, null);
      }
      area.dispatchEvent(new Event('input', { bubbles: true }));
    }));
  }
  form.querySelectorAll('[data-rte]').forEach(setupRte);
  form.addEventListener('input', () => { dirty = true; });
  form.addEventListener('change', () => { dirty = true; });

  // ---- Urutan dan hapus opsi ----
  const rows = () => [...form.querySelectorAll('[data-row="choice"]')];
  function swapRows(a, b) {
    const ea = a.querySelector('.rte-area'), eb = b.querySelector('.rte-area');
    [ea.innerHTML, eb.innerHTML] = [eb.innerHTML, ea.innerHTML];
    const ca = a.querySelector('[data-correct]'), cb = b.querySelector('[data-correct]');
    [ca.checked, cb.checked] = [cb.checked, ca.checked];
    [ea, eb].forEach((e) => { renderAllMath(e); e.dispatchEvent(new Event('input', { bubbles: true })); });
  }
  form.querySelectorAll('[data-move]').forEach((btn) => btn.addEventListener('click', () => {
    const list = rows(), row = btn.closest('[data-row]'), i = list.indexOf(row);
    const j = btn.dataset.move === 'up' ? i - 1 : i + 1;
    if (j >= 0 && j < list.length && list[j].offsetParent !== null) swapRows(row, list[j]);
  }));
  form.querySelectorAll('[data-remove-last]').forEach((btn) => btn.addEventListener('click', () => {
    const visible = rows().filter((r) => r.offsetParent !== null);
    const last = visible[visible.length - 1];
    if (!last) return;
    last.querySelector('.rte-area').innerHTML = '';
    last.querySelector('[data-correct]').checked = false;
    last.querySelector('.rte-area').dispatchEvent(new Event('input', { bubbles: true }));
  }));

  // ---- Pintasan keyboard ----
  document.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') { e.preventDefault(); form.requestSubmit(); }
    if (e.altKey && /^[1-6]$/.test(e.key)) {
      const box = form.querySelector('[data-correct="' + (Number(e.key) - 1) + '"]');
      if (box && box.offsetParent !== null) { e.preventDefault(); box.checked = box.type === 'radio' ? true : !box.checked; dirty = true; }
    }
  });

  // ---- Pratinjau (dirender server, komponen yang sama dengan halaman ujian) ----
  const frame = form.querySelector('[data-preview-frame]');
  form.querySelector('[data-preview]')?.addEventListener('click', async () => {
    form.querySelectorAll('[data-rte]').forEach((b) => { b.querySelector('textarea').value = serialize(b.querySelector('.rte-area')); });
    const fd = new FormData(form);
    try {
      const res = await fetch('/ujian/pratinjau', { method: 'POST', body: new URLSearchParams(fd), headers: { 'X-CSRF-Token': csrf() }, credentials: 'same-origin' });
      frame.innerHTML = await res.text();
    } catch (e) {
      frame.textContent = 'Pratinjau gagal dimuat. Periksa koneksi.';
    }
  });
  form.querySelectorAll('[data-preview-width]').forEach((b) => b.addEventListener('click', () => {
    frame.classList.toggle('is-phone', b.dataset.previewWidth === 'phone');
  }));

  // ---- Draf otomatis tiap 5 detik (browser dan server) ----
  if (form.dataset.question !== undefined && document.querySelector('[data-save-status]')) {
    const key = 'akra-draf:' + form.dataset.bank + ':' + (form.dataset.question || 'baru');
    const savedAt = Number(form.dataset.savedAt || 0) * 1000;
    try {
      const local = JSON.parse(localStorage.getItem(key) || 'null');
      if (local && local.at > savedAt && !document.querySelector('[data-restored]')) {
        setStatus('Ada draf di perangkat ini dari ' + new Date(local.at).toLocaleTimeString('id-ID') + '.');
      }
    } catch (e) { /* localStorage tidak tersedia */ }
    let saving = false;
    setInterval(async () => {
      if (!dirty || saving) return;
      dirty = false;
      saving = true;
      form.querySelectorAll('[data-rte]').forEach((b) => { b.querySelector('textarea').value = serialize(b.querySelector('.rte-area')); });
      const body = new URLSearchParams(new FormData(form));
      body.delete('csrf_token');
      body.set('bank_id', form.dataset.bank);
      body.set('question_id', form.dataset.question || '');
      try { localStorage.setItem(key, JSON.stringify({ at: Date.now(), data: body.toString() })); } catch (e) { /* penuh/diblokir */ }
      try {
        const res = await fetch('/ujian/draf', { method: 'POST', body, headers: { 'X-CSRF-Token': csrf() }, credentials: 'same-origin' });
        setStatus(res.ok ? 'Draf tersimpan ' + new Date().toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit' }) : 'Draf hanya tersimpan di perangkat ini.');
      } catch (e) {
        setStatus('Offline — draf tersimpan di perangkat ini.');
      }
      saving = false;
    }, 5000);
    form.addEventListener('submit', () => {
      form.querySelectorAll('[data-rte]').forEach((b) => { b.querySelector('textarea').value = serialize(b.querySelector('.rte-area')); });
      try { localStorage.removeItem(key); } catch (e) { /* abaikan */ }
      dirty = false;
    });
  } else {
    form.addEventListener('submit', () => {
      form.querySelectorAll('[data-rte]').forEach((b) => { b.querySelector('textarea').value = serialize(b.querySelector('.rte-area')); });
    });
  }
})();
