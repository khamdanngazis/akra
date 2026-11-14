// Unggah dokumen pendaftaran otomatis (T108). File langsung diunggah saat
// dipilih; foto dikecilkan dulu di browser (sisi terpanjang 1600 px, JPEG)
// agar foto kamera HP 3–8 MB muat di batas 2 MB. Server tetap memeriksa
// tipe dan ukuran (T91). Tanpa JavaScript, form biasa dengan tombol Unggah
// tetap bekerja.
(function () {
  const MAX_BYTES = 2 * 1024 * 1024;
  const MAX_SIDE = 1600;
  const SHRINK_ABOVE = 1024 * 1024; // foto kecil dikirim apa adanya

  // shrink mengecilkan foto menjadi JPEG. File selain gambar, atau gambar
  // yang tidak bisa dibaca browser (mis. HEIC di Chrome), dikembalikan apa
  // adanya; server yang memutuskan.
  async function shrink(file) {
    if (!file.type.startsWith('image/')) return file;
    const plain = file.type === 'image/jpeg' || file.type === 'image/png';
    if (plain && file.size <= SHRINK_ABOVE) return file;
    let bmp;
    try {
      bmp = await createImageBitmap(file, { imageOrientation: 'from-image' });
    } catch (e) {
      return file;
    }
    const scale = Math.min(1, MAX_SIDE / Math.max(bmp.width, bmp.height));
    const canvas = document.createElement('canvas');
    canvas.width = Math.round(bmp.width * scale);
    canvas.height = Math.round(bmp.height * scale);
    const ctx = canvas.getContext('2d');
    ctx.fillStyle = '#fff'; // latar putih untuk PNG transparan
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(bmp, 0, 0, canvas.width, canvas.height);
    if (bmp.close) bmp.close();
    const toBlob = (q) => new Promise((res) => canvas.toBlob(res, 'image/jpeg', q));
    let blob = await toBlob(0.8);
    if (blob && blob.size > MAX_BYTES * 0.95) blob = await toBlob(0.6);
    if (!blob) return file;
    const name = (file.name || 'foto').replace(/\.[^.]*$/, '') + '.jpg';
    return new File([blob], name, { type: 'image/jpeg' });
  }

  function csrfToken() {
    const m = document.querySelector('meta[name="csrf-token"]');
    return m ? m.content : '';
  }

  // send mengunggah lewat XHR (fetch belum punya progres unggah).
  function send(url, file, onProgress) {
    return new Promise((resolve) => {
      const xhr = new XMLHttpRequest();
      xhr.open('POST', url);
      xhr.setRequestHeader('Accept', 'application/json');
      xhr.setRequestHeader('X-CSRF-Token', csrfToken());
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100));
      };
      xhr.onload = () => {
        let body = null;
        try {
          body = JSON.parse(xhr.responseText);
        } catch (e) {
          body = null;
        }
        if (!body) {
          resolve({ ok: false, message: 'Sesi berakhir atau halaman kedaluwarsa. Muat ulang halaman ini lalu coba lagi.' });
          return;
        }
        resolve(body);
      };
      xhr.onerror = () => resolve({ ok: false, message: 'Koneksi terputus. Periksa internet lalu coba lagi.' });
      const fd = new FormData();
      fd.append('file', file);
      xhr.send(fd);
    });
  }

  // docUpload adalah komponen Alpine satu kartu dokumen. Status awal dari
  // server: data-state ("ok", "error", atau "empty") dan data-message.
  window.docUpload = function (el) {
    return {
      url: el.dataset.url,
      state: el.dataset.state || 'empty',
      message: el.dataset.message || '',
      progress: 0,
      preview: '',
      fileName: '',
      last: null,
      init() {
        el.dataset.docState = this.state;
        this.$watch('state', (v) => (el.dataset.docState = v));
      },
      async pick(event) {
        const input = event.target;
        const file = input.files && input.files[0];
        input.value = ''; // pilih file yang sama lagi tetap memicu change
        if (!file) return;
        await this.upload(file);
      },
      async upload(file) {
        this.last = file;
        this.state = 'uploading';
        this.progress = 0;
        this.message = file.type.startsWith('image/') && file.size > SHRINK_ABOVE ? 'Mengecilkan foto…' : 'Mengunggah…';
        const ready = await shrink(file);
        if (ready.size > MAX_BYTES) {
          this.state = 'error';
          this.message = ready.type === 'application/pdf'
            ? 'PDF lebih dari 2 MB. Foto dokumennya dengan kamera HP, atau perkecil PDF-nya.'
            : 'File lebih dari 2 MB dan tidak bisa dikecilkan. Coba foto ulang dokumennya.';
          return;
        }
        this.message = 'Mengunggah…';
        const res = await send(this.url, ready, (p) => (this.progress = p));
        if (res.ok) {
          if (this.preview) URL.revokeObjectURL(this.preview);
          this.preview = ready.type.startsWith('image/') ? URL.createObjectURL(ready) : '';
          this.fileName = file.name || '';
          this.state = 'ok';
          this.message = 'Terunggah' + (this.fileName ? ' · ' + this.fileName : '');
          this.last = null;
        } else {
          this.state = 'error';
          this.message = 'Gagal: ' + (res.message || 'coba lagi.');
        }
      },
      retry() {
        if (this.last) this.upload(this.last);
      },
    };
  };

  // docUploadBlocker mengembalikan peringatan jika masih ada unggahan yang
  // berjalan atau gagal di halaman ini, atau '' jika aman lanjut.
  window.docUploadBlocker = function () {
    const busy = document.querySelectorAll('[data-doc-state="uploading"]').length;
    if (busy) return 'Masih ada ' + busy + ' dokumen yang sedang diunggah. Tunggu sampai selesai.';
    const failed = document.querySelectorAll('[data-doc-state="error"]').length;
    if (failed) return failed + ' dokumen gagal diunggah. Coba lagi, atau lanjut dan unggah nanti.';
    return '';
  };
})();
