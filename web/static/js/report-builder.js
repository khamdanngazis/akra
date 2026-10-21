// Komponen Alpine pembangun laporan (T78). Hanya mengatur tampilan: pilihan
// sumber, urutan kolom, dan baris filter/urutan. Semua akses dan validasi
// tetap di server (pratinjau dan simpan memeriksa ulang).
window.reportBuilder = function (el) {
  const st = JSON.parse(el.dataset.state);
  return {
    ...st,
    get current() {
      return this.sources.find((s) => s.name === this.source) || { columns: [] };
    },
    column(key) {
      return this.current.columns.find((c) => c.key === key);
    },
    label(key) {
      const c = this.column(key);
      return c ? c.label : key;
    },
    opsFor(key) {
      const c = this.column(key);
      return c ? this.ops[c.type] || [] : [];
    },
    inputType(f) {
      const c = this.column(f.column);
      if (!c || f.op === 'in') return 'text';
      return c.type === 'date' ? 'date' : 'text';
    },
    placeholder(f) {
      const c = this.column(f.column);
      if (f.op === 'in') return 'Pisahkan dengan koma';
      if (!c) return 'Nilai';
      return { date: 'YYYY-MM-DD', int: 'Angka', money: 'Rupiah, mis. 150000' }[c.type] || 'Teks';
    },
    // Sumber berganti: pilihan lama tidak berlaku untuk sumber lain.
    changeSource() {
      this.columns = [];
      this.filters = [];
      this.sort = [];
    },
    selected(key) {
      return this.columns.includes(key);
    },
    toggle(key) {
      if (this.selected(key)) {
        this.columns = this.columns.filter((k) => k !== key);
      } else if (this.columns.length < this.maxColumns) {
        this.columns.push(key);
      }
    },
    move(i, d) {
      const j = i + d;
      if (j < 0 || j >= this.columns.length) return;
      const cols = [...this.columns];
      [cols[i], cols[j]] = [cols[j], cols[i]];
      this.columns = cols;
    },
    addFilter() {
      if (this.filters.length >= this.maxFilters) return;
      const first = this.current.columns[0];
      this.filters.push({ column: first ? first.key : '', op: '=', value: '' });
    },
    // Operator yang tidak cocok dengan tipe kolom baru diganti operator pertama.
    fixOp(f) {
      const ops = this.opsFor(f.column).map((o) => o.value);
      if (!ops.includes(f.op)) f.op = ops[0] || '=';
    },
    addSort() {
      if (this.sort.length >= this.maxSort) return;
      const first = this.current.columns[0];
      this.sort.push({ column: first ? first.key : '', desc: false });
    },
  };
};
