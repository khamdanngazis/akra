// Isian alamat bertingkat Provinsi → Kabupaten/Kota → Kecamatan →
// Desa/Kelurahan (T110). Daftar diambil dari endpoint AKRA sendiri
// (/wilayah?induk=), bukan layanan pihak ketiga. Memilih desa mengisi kode
// pos. Jika daftar gagal dimuat, isian jatuh ke mode manual.
window.wilayahPicker = function (el) {
  const cache = (window.__wilayahCache = window.__wilayahCache || {});
  async function fetchList(induk) {
    if (cache[induk]) return cache[induk];
    const res = await fetch('/wilayah?induk=' + encodeURIComponent(induk), { headers: { Accept: 'application/json' } });
    if (!res.ok) throw new Error('wilayah ' + res.status);
    const list = await res.json();
    cache[induk] = list;
    return list;
  }
  // prefix mengembalikan kode wilayah tingkat n dari kode desa.
  const prefix = (kode, n) => kode.split('.').slice(0, n).join('.');

  return {
    prov: '',
    kab: '',
    kec: '',
    desa: '',
    q: '',
    pos: (el.querySelector('[name="postal_code"]') || {}).value || '',
    manual: el.dataset.manual === 'true',
    failed: false,
    loading: 0,
    lists: { 1: [], 2: [], 3: [], 4: [] },
    get desaOptions() {
      const q = this.q.trim().toLowerCase();
      if (!q) return this.lists[4];
      return this.lists[4].filter((o) => o.nama.toLowerCase().includes(q) || o.kode === this.desa);
    },
    async load(level, induk) {
      this.loading = level;
      try {
        this.lists[level] = await fetchList(induk);
      } catch (e) {
        this.failed = true;
        this.manual = true;
      } finally {
        this.loading = 0;
      }
      await this.$nextTick();
    },
    async init() {
      await this.load(1, '');
      const kode = el.dataset.kode || el.dataset.suggest || '';
      if (!kode || this.failed) return;
      this.prov = prefix(kode, 1);
      await this.load(2, this.prov);
      this.kab = prefix(kode, 2);
      await this.load(3, this.kab);
      this.kec = prefix(kode, 3);
      await this.load(4, this.kec);
      this.desa = kode;
      if (el.dataset.suggest && !el.dataset.kode) this.fillPos();
    },
    // pick dipanggil saat pilihan tingkat level berubah: kosongkan tingkat
    // di bawahnya lalu muat daftarnya.
    async pick(level) {
      const keys = ['', 'prov', 'kab', 'kec', 'desa'];
      for (let l = level + 1; l <= 4; l++) {
        this[keys[l]] = '';
        this.lists[l] = [];
      }
      if (level === 3) this.q = '';
      if (level < 4 && this[keys[level]]) await this.load(level + 1, this[keys[level]]);
      if (level === 4) this.fillPos();
    },
    fillPos() {
      const d = this.lists[4].find((o) => o.kode === this.desa);
      if (d && d.kode_pos) this.pos = d.kode_pos;
    },
  };
};
