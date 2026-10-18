// Uji beban ringan AKRA (T61). Data dari cmd/loadseed (loadtest/data.json).
//
//   k6 run -e BASE_URL=http://localhost:8080 loadtest/scenarios.js
//   k6 run -e LOAD_FACTOR=2 ... (beban 2x)
//
// Skenario (per LOAD_FACTOR):
//   browse:  50 pengguna membuka beranda dan daftar siswa dengan pencarian
//   arrears: 10 pengguna membuka daftar tunggakan dengan filter kelas
//   export:   5 pengguna mengunduh rekap kehadiran satu kelas (.xlsx)
import http from 'k6/http';
import { check, sleep, fail } from 'k6';
import { Counter } from 'k6/metrics';
import { SharedArray } from 'k6/data';

const BASE = (__ENV.BASE_URL || 'http://localhost:8080').replace(/\/$/, '');
const FACTOR = Number(__ENV.LOAD_FACTOR || 1);
const DURATION = __ENV.DURATION || '5m';
const data = JSON.parse(open(__ENV.DATA || './data.json'));
const schools = new SharedArray('schools', () => data.schools);

const serverErrors = new Counter('server_errors');

export const options = {
  // Sesi login dipertahankan antar-iterasi, seperti browser sungguhan
  // (bawaan k6 mengosongkan cookie setiap iterasi).
  noCookiesReset: true,
  scenarios: {
    browse: { executor: 'constant-vus', exec: 'browse', vus: 50 * FACTOR, duration: DURATION },
    arrears: { executor: 'constant-vus', exec: 'arrears', vus: 10 * FACTOR, duration: DURATION },
    export: { executor: 'constant-vus', exec: 'exportAttendance', vus: 5 * FACTOR, duration: DURATION },
  },
  thresholds: {
    // Kriteria T61: median halaman biasa < 500 ms, tanpa 5xx.
    'http_req_duration{page:home}': ['med<500', 'p(95)<2000'],
    'http_req_duration{page:students}': ['med<500', 'p(95)<2000'],
    'http_req_duration{page:arrears}': ['med<500', 'p(95)<2000'],
    'http_req_duration{page:attendance_xlsx}': ['med<2000', 'p(95)<5000'],
    'http_req_duration{page:login}': ['med<2000'],
    server_errors: ['count==0'],
  },
  summaryTrendStats: ['med', 'p(95)', 'p(99)', 'max', 'count'],
};

function track(res) {
  if (res.status >= 500) serverErrors.add(1);
  return res;
}

// login memakai form sungguhan: token CSRF dari halaman login, cookie sesi
// disimpan cookie jar k6 per pengguna virtual.
function login(school, email) {
  const page = track(http.get(`${BASE}/login`, { tags: { page: 'login' } }));
  const m = page.body && page.body.match(/name="csrf_token" value="([^"]+)"/);
  if (!m) fail(`no csrf token on /login (status ${page.status})`);
  const token = m[1].replace(/&#43;/g, '+');
  const res = track(http.post(`${BASE}/login`, {
    school_code: school.code, email, password: data.password, csrf_token: token,
  }, { redirects: 0, tags: { page: 'login' }, headers: { Origin: BASE, Referer: `${BASE}/login` } }));
  if (!check(res, { 'login redirects': (r) => r.status === 303 })) {
    fail(`login ${email} failed: status ${res.status}`);
  }
}

// Setiap pengguna virtual memakai sekolah dan akun sesuai nomor VU.
let loggedIn = false;
function ensureLogin(pickEmail) {
  if (loggedIn) return;
  const school = schools[(__VU - 1) % schools.length];
  login(school, pickEmail(school));
  loggedIn = true;
}

// inApp memastikan respons adalah halaman aplikasi (bukan halaman login
// setelah redirect karena sesi hilang).
function inApp(r) {
  return r.status === 200 && r.url.indexOf('/login') === -1 && r.body.indexOf('id="app-nav"') !== -1;
}

function pick(list) {
  return list[Math.floor(Math.random() * list.length)];
}

export function browse() {
  ensureLogin((s) => (__VU % 2 === 0 ? s.admin : s.principal));
  const school = schools[(__VU - 1) % schools.length];
  const home = track(http.get(`${BASE}/`, { tags: { page: 'home' } }));
  check(home, { 'home 200': inApp });
  sleep(1 + Math.random());
  const q = encodeURIComponent(pick(school.search));
  const list = track(http.get(`${BASE}/siswa/?q=${q}`, { tags: { page: 'students' } }));
  check(list, { 'students 200': (r) => inApp(r) && r.body.indexOf('Menampilkan') !== -1 || r.body.indexOf('Tidak ada siswa') !== -1 });
  sleep(1 + Math.random());
}

export function arrears() {
  ensureLogin((s) => s.treasurer);
  const school = schools[(__VU - 1) % schools.length];
  const cls = Math.random() < 0.3 ? '' : pick(school.classes).id;
  const res = track(http.get(`${BASE}/keuangan/tunggakan?kelas=${cls}`, { tags: { page: 'arrears' } }));
  check(res, { 'arrears 200': inApp });
  sleep(2 + Math.random() * 2);
}

export function exportAttendance() {
  ensureLogin((s) => s.admin);
  const school = schools[(__VU - 1) % schools.length];
  const cls = pick(school.classes).id;
  const res = track(http.get(`${BASE}/laporan/kehadiran.xlsx?kelas=${cls}&dari=${data.from}&sampai=${data.to}`,
    { tags: { page: 'attendance_xlsx' } }));
  const ok = check(res, { 'export 200 xlsx': (r) => r.status === 200 && String(r.headers['Content-Type']).includes('spreadsheet') });
  if (!ok && __ENV.DEBUG) console.log(`export ${res.status} ${res.headers['Content-Type']} ${res.url} ${String(res.body).slice(0, 200)}`);
  sleep(5 + Math.random() * 5);
}
