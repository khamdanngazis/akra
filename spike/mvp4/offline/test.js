// Uji offline di Chrome headless (desktop + emulasi Android).
const puppeteer = require('puppeteer-core');
const srv = require('./server');
const assert = (c, m) => { if (!c) { console.log('FAIL', m); process.exitCode = 1; } else console.log('ok  ', m); };
(async () => {
  await new Promise(r => srv.listen(8099, r));
  const b = await puppeteer.launch({ executablePath: process.env.CHROME || '/usr/bin/google-chrome', args: ['--no-sandbox'] });
  const p = await b.newPage();
  await p.emulate(puppeteer.KnownDevices['Galaxy S8']);
  await p.goto('http://localhost:8099/ujian.html', { waitUntil: 'networkidle0' });
  await p.waitForFunction('window.__ready');
  await p.evaluate(() => navigator.serviceWorker.ready);
  await p.reload({ waitUntil: 'networkidle0' }); // halaman kini dikendalikan SW
  await p.waitForFunction('window.__ready');
  assert(await p.evaluate(() => !!navigator.serviceWorker.controller), 'service worker controls page');
  await p.click('input[name=q1][value=B]');
  await p.waitForFunction(() => document.getElementById('status').textContent === 'Tersimpan');
  await p.setOfflineMode(true);
  await p.reload({ waitUntil: 'domcontentloaded' });
  await p.waitForFunction('window.__ready', { timeout: 5000 });
  assert(await p.evaluate(() => document.querySelectorAll('#soal section').length) === 5, 'reload while offline renders 5 questions from cache');
  assert(await p.evaluate(() => document.querySelector('#soal img').complete && document.querySelector('#soal img').naturalWidth > 0), 'image served from cache offline');
  await p.click('input[name=q2][value=C]');
  await p.click('input[name=q3][value=D]');
  await p.waitForFunction(() => /menunggu/.test(document.getElementById('status').textContent));
  assert(true, 'offline answers queued: ' + await p.evaluate(() => document.getElementById('status').textContent));
  // tutup tab saat offline, buka lagi saat online: antrean tetap ada
  await p.close();
  const p2 = await b.newPage();
  await p2.goto('http://localhost:8099/ujian.html', { waitUntil: 'networkidle0' });
  await p2.waitForFunction(() => document.getElementById('status').textContent === 'Tersimpan', { timeout: 15000 });
  const saved = await (await fetch('http://localhost:8099/api/_saved')).json();
  assert(saved[1]?.v === 'B' && saved[2]?.v === 'C' && saved[3]?.v === 'D', 'outbox flushed after tab closed offline and reopened online: ' + JSON.stringify(Object.values(saved).map(x => x.q + x.v)));
  await b.close(); srv.close();
})().catch(e => { console.error(e); process.exit(1); });
