// Prototipe spike T123: payload soal sekali muat, jawaban ke antrean
// IndexedDB (outbox), dikirim saat online. Bukan kode produksi.
(function () {
  const st = document.getElementById('status');
  const setStatus = (s, t) => { st.dataset.s = s; st.textContent = t; };
  let db, payload, seq = Number(localStorage.getItem('seq') || 0);
  function idb() {
    return new Promise((res, rej) => {
      const r = indexedDB.open('ujian-spike', 1);
      r.onupgradeneeded = () => r.result.createObjectStore('outbox', { keyPath: 'q' });
      r.onsuccess = () => res(r.result); r.onerror = () => rej(r.error);
    });
  }
  const tx = (mode, f) => new Promise((res, rej) => {
    const t = db.transaction('outbox', mode); const out = f(t.objectStore('outbox'));
    t.oncomplete = () => res(out && out.result); t.onerror = () => rej(t.error);
  });
  async function flush() {
    const items = await tx('readonly', s => s.getAll());
    if (!items.length) { setStatus(navigator.onLine ? 'ok' : 'offline', navigator.onLine ? 'Tersimpan' : 'Offline — jawaban aman'); return; }
    try {
      const r = await fetch('/api/save', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ a: items }) });
      if (!r.ok) throw new Error(r.status);
      const ack = await r.json();
      // hapus hanya yang seq-nya sudah dikonfirmasi server
      await tx('readwrite', s => { items.forEach(i => { if (i.s <= ack.s) s.delete(i.q); }); });
      setStatus('ok', 'Tersimpan');
    } catch (e) {
      setStatus('offline', 'Offline — ' + items.length + ' jawaban menunggu dikirim');
    }
  }
  async function answer(q, v) {
    seq++; localStorage.setItem('seq', seq);
    await tx('readwrite', s => s.put({ q, v, s: seq }));
    document.querySelector('#grid [data-q="' + q + '"]').classList.add('done');
    flush();
  }
  function render() {
    const m = document.getElementById('soal'), g = document.getElementById('grid');
    m.innerHTML = ''; g.innerHTML = '';
    payload.soal.forEach(s => {
      const sec = document.createElement('section');
      sec.innerHTML = '<p><b>' + s.no + '.</b> ' + s.html + '</p>' + (s.img ? '<img alt="" width="160" src="' + s.img + '">' : '');
      s.opsi.forEach((o, i) => {
        const l = document.createElement('label'); l.className = 'opt';
        const k = 'ABCDE'[i];
        l.innerHTML = '<input type="radio" name="q' + s.no + '" value="' + k + '"> ' + k + '. ' + o;
        l.querySelector('input').onchange = () => answer(s.no, k);
        sec.appendChild(l);
      });
      m.appendChild(sec);
      const b = document.createElement('button'); b.textContent = s.no; b.dataset.q = s.no; g.appendChild(b);
    });
  }
  async function start() {
    db = await idb();
    if ('serviceWorker' in navigator) navigator.serviceWorker.register('sw.js');
    const r = await fetch('/api/payload');
    payload = await r.json();
    render();
    flush();
    window.addEventListener('online', flush);
    setInterval(flush, 10000);
    window.__ready = true;
  }
  start().catch(e => setStatus('offline', 'Gagal memuat: ' + e.message));
})();
