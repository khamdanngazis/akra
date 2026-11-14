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
    q: '', // teks di isian desa: nama desa terpilih, atau kata yang diketik
    open: false,
    active: 0,
    pos: (el.querySelector('[name="postal_code"]') || {}).value || '',
    manual: el.dataset.manual === 'true',
    failed: false,
    loading: 0,
    lists: { 1: [], 2: [], 3: [], 4: [] },
    get desaName() {
      const d = this.lists[4].find((o) => o.kode === this.desa);
      return d ? d.nama : '';
    },
    // desaOptions: semua desa jika isian masih berisi nama desa terpilih
    // (atau kosong), selain itu yang namanya memuat kata yang diketik; yang
    // diawali kata itu diurutkan lebih dulu.
    get desaOptions() {
      const q = this.q.trim().toLowerCase();
      if (!q || (this.desa && this.q === this.desaName)) return this.lists[4];
      const hits = this.lists[4].filter((o) => o.nama.toLowerCase().includes(q));
      return hits.sort((a, b) => (b.nama.toLowerCase().startsWith(q) ? 1 : 0) - (a.nama.toLowerCase().startsWith(q) ? 1 : 0));
    },
    openDesa() {
      if (!this.kec) return;
      this.open = true;
      const i = this.desaOptions.findIndex((o) => o.kode === this.desa);
      this.active = i >= 0 ? i : 0;
    },
    // closeDesa menutup daftar; teks yang diketik tanpa memilih dikembalikan
    // ke nama desa terpilih (atau dikosongkan).
    closeDesa() {
      this.open = false;
      this.q = this.desaName;
    },
    // typed membuka daftar hasil. Desa terpilih sebelumnya tetap tersimpan
    // sampai desa lain dipilih; jika batal, teks kembali ke nama desa itu.
    typed() {
      this.open = true;
      this.active = 0;
    },
    move(step) {
      if (!this.open) return this.openDesa();
      const n = this.desaOptions.length;
      if (n) this.active = (this.active + step + n) % n;
      this.$nextTick(() => {
        const li = el.querySelector('.wilayah-options li.is-active');
        if (li) li.scrollIntoView({ block: 'nearest' });
      });
    },
    chooseActive() {
      const o = this.desaOptions[this.active];
      if (this.open && o) this.choose(o);
    },
    choose(o) {
      this.desa = o.kode;
      this.q = o.nama;
      this.open = false;
      this.fillPos();
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
      this.q = this.desaName;
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
      if (level <= 3) {
        this.q = '';
        this.open = false;
      }
      if (level < 4 && this[keys[level]]) await this.load(level + 1, this[keys[level]]);
      if (level === 4) this.fillPos();
    },
    fillPos() {
      const d = this.lists[4].find((o) => o.kode === this.desa);
      if (d && d.kode_pos) this.pos = d.kode_pos;
    },
  };
};
