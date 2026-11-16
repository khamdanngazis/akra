// Server uji spike: menyajikan public/, payload, gambar, dan /api/save.
const http = require('http'), fs = require('fs'), path = require('path');
const saved = {};
const payload = { soal: Array.from({ length: 5 }, (_, i) => ({ no: i + 1, html: 'Soal nomor ' + (i + 1), img: i === 0 ? '/img/gambar.svg' : '', opsi: ['satu', 'dua', 'tiga', 'empat'] })) };
const srv = http.createServer((req, res) => {
  if (req.url === '/api/payload') { res.setHeader('Content-Type', 'application/json'); return res.end(JSON.stringify(payload)); }
  if (req.url === '/img/gambar.svg') { res.setHeader('Content-Type', 'image/svg+xml'); return res.end('<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>'); }
  if (req.url === '/api/save' && req.method === 'POST') {
    let b = ''; req.on('data', d => b += d); req.on('end', () => {
      const { a } = JSON.parse(b); let max = 0;
      a.forEach(x => { if (!saved[x.q] || saved[x.q].s < x.s) saved[x.q] = x; max = Math.max(max, x.s); });
      res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify({ s: max }));
    }); return;
  }
  if (req.url === '/api/_saved') return res.end(JSON.stringify(saved));
  const f = path.join(__dirname, 'public', path.normalize(req.url.split('?')[0]).replace(/^\/+/, '') || 'ujian.html');
  fs.readFile(f, (err, d) => {
    if (err) { res.statusCode = 404; return res.end(); }
    res.setHeader('Content-Type', { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css' }[path.extname(f)] || 'application/octet-stream');
    res.end(d);
  });
});
module.exports = srv;
if (require.main === module) srv.listen(8099);
