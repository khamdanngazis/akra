// Titik lokasi rumah (T106, T112): tombol "Ambil lokasi saya" (GPS
// perangkat) dan, jika API key Google Maps diset, "Pilih di peta". Skrip
// Google baru dimuat saat tombol peta ditekan, jadi tidak ada permintaan ke
// Google selama orang tua tidak membuka peta. Yang dilihat Google hanya
// tampilan peta; nama dan isian lain tidak dikirim. Validasi koordinat
// tetap di server.
(function () {
  const INDONESIA = { lat: -2.5, lng: 118, zoom: 5 };
  const MIN_SAVE_ZOOM = 16;
  let mapsPromise = null;

  // loadMaps memuat Maps JavaScript API sekali per halaman.
  function loadMaps(key) {
    if (window.google && window.google.maps && window.google.maps.importLibrary) return Promise.resolve();
    if (mapsPromise) return mapsPromise;
    mapsPromise = new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('timeout')), 20000);
      window.__akraMapsReady = () => {
        clearTimeout(timer);
        resolve();
      };
      // Dipanggil Google jika kunci ditolak (salah, referrer, kuota).
      window.gm_authFailure = () => {
        clearTimeout(timer);
        reject(new Error('auth'));
        window.dispatchEvent(new Event('akra-maps-auth-failure'));
      };
      const s = document.createElement('script');
      s.src = 'https://maps.googleapis.com/maps/api/js?key=' + encodeURIComponent(key) +
        '&loading=async&callback=__akraMapsReady&language=id&region=ID&v=weekly';
      s.async = true;
      s.onerror = () => {
        clearTimeout(timer);
        reject(new Error('load'));
      };
      document.head.appendChild(s);
    }).catch((e) => {
      mapsPromise = null; // boleh dicoba lagi
      throw e;
    });
    return mapsPromise;
  }

  async function wilayahList(induk) {
    const cache = (window.__wilayahCache = window.__wilayahCache || {});
    if (cache[induk]) return cache[induk];
    const res = await fetch('/wilayah?induk=' + encodeURIComponent(induk), { headers: { Accept: 'application/json' } });
    if (!res.ok) throw new Error('wilayah ' + res.status);
    cache[induk] = await res.json();
    return cache[induk];
  }

  // regionCenter mencari titik kabupaten/kota (atau provinsi) dari isian
  // wilayah di form yang sama (T110).
  async function regionCenter(form) {
    if (!form) return null;
    const val = (sel) => {
      const e = form.querySelector(sel);
      return e ? e.value : '';
    };
    const desa = val('input[name="wilayah_kode"]');
    const kab = val('select[id$="-wilayah-kab"]') || (desa ? desa.split('.').slice(0, 2).join('.') : '');
    const prov = val('select[id$="-wilayah-prov"]') || (kab ? kab.split('.')[0] : '');
    try {
      if (kab && prov) {
        const k = (await wilayahList(prov)).find((o) => o.kode === kab);
        if (k && k.lat) return { lat: +k.lat, lng: +k.lng, zoom: 13 };
      }
      if (prov) {
        const p = (await wilayahList('')).find((o) => o.kode === prov);
        if (p && p.lat) return { lat: +p.lat, lng: +p.lng, zoom: 9 };
      }
    } catch (e) {
      return null;
    }
    return null;
  }

  window.lokasiPicker = function (el) {
    return {
      lat: el.dataset.lat || '',
      lng: el.dataset.lng || '',
      state: '',
      msg: '',
      can: window.isSecureContext && 'geolocation' in navigator,
      mapsKey: el.dataset.mapsKey || '',
      mapOpen: false,
      mapLoading: false,
      mapError: '',
      zoom: 0,
      map: null,
      init() {
        // Kunci ditolak Google setelah skrip termuat (salah, referrer, kuota).
        window.addEventListener('akra-maps-auth-failure', () => {
          this.mapError = 'Peta tidak bisa dibuka saat ini (layanan peta menolak permintaan). Gunakan "Ambil lokasi saya" atau kosongkan.';
          this.mapLoading = false;
        });
      },
      get viewUrl() {
        return 'https://www.google.com/maps/search/?api=1&query=' + encodeURIComponent(this.lat + ',' + this.lng);
      },
      locate() {
        this.state = 'busy';
        this.msg = 'Mengambil lokasi…';
        navigator.geolocation.getCurrentPosition((p) => {
          this.lat = p.coords.latitude.toFixed(6);
          this.lng = p.coords.longitude.toFixed(6);
          this.state = 'ok';
          this.msg = 'Lokasi terisi (akurasi sekitar ' + Math.round(p.coords.accuracy) + ' meter). Jika sedang tidak di rumah, pilih titiknya di peta.';
        }, (e) => {
          this.state = 'error';
          this.msg = e.code === 1 ? 'Izin lokasi ditolak. Izinkan akses lokasi untuk situs ini di pengaturan browser, atau pilih titik di peta. Boleh juga dikosongkan.'
            : e.code === 2 ? 'Lokasi tidak tersedia. Pastikan GPS/lokasi di HP aktif, lalu coba lagi.'
            : 'Waktu habis saat mengambil lokasi. Coba lagi di dekat jendela atau di luar ruangan.';
        }, { enableHighAccuracy: true, timeout: 15000, maximumAge: 0 });
      },
      clear() {
        this.lat = '';
        this.lng = '';
        this.state = '';
        this.msg = '';
      },
      async openMap() {
        this.mapOpen = true;
        this.mapError = '';
        document.body.style.overflow = 'hidden';
        let start = INDONESIA;
        if (this.lat && this.lng) start = { lat: +this.lat, lng: +this.lng, zoom: 18 };
        else start = (await regionCenter(el.closest('form'))) || INDONESIA;
        if (this.map) {
          this.map.setCenter({ lat: start.lat, lng: start.lng });
          this.map.setZoom(start.zoom);
          return;
        }
        this.mapLoading = true;
        try {
          await loadMaps(this.mapsKey);
          const { Map } = await google.maps.importLibrary('maps');
          await this.$nextTick();
          this.map = new Map(el.querySelector('.lokasi-map'), {
            center: { lat: start.lat, lng: start.lng },
            zoom: start.zoom,
            mapTypeControl: true,
            mapTypeControlOptions: { mapTypeIds: ['roadmap', 'hybrid'] },
            streetViewControl: false,
            fullscreenControl: false,
            clickableIcons: false,
            gestureHandling: 'greedy',
          });
          this.zoom = this.map.getZoom();
          this.map.addListener('zoom_changed', () => (this.zoom = this.map.getZoom()));
          // Mengetuk peta memindahkan titik ke tempat yang diketuk.
          this.map.addListener('click', (e) => this.map.panTo(e.latLng));
        } catch (e) {
          this.mapError = e && e.message === 'auth'
            ? 'Peta tidak bisa dibuka saat ini (layanan peta menolak permintaan). Gunakan "Ambil lokasi saya" atau kosongkan.'
            : 'Peta gagal dimuat. Periksa koneksi internet lalu coba lagi, atau gunakan "Ambil lokasi saya".';
        } finally {
          this.mapLoading = false;
        }
      },
      closeMap() {
        this.mapOpen = false;
        document.body.style.overflow = '';
      },
      useMapPoint() {
        if (!this.map || this.zoom < MIN_SAVE_ZOOM) return;
        const c = this.map.getCenter();
        this.lat = c.lat().toFixed(6);
        this.lng = c.lng().toFixed(6);
        this.state = 'ok';
        this.msg = 'Titik rumah dipilih di peta.';
        this.closeMap();
      },
      get canSave() {
        return !!this.map && this.zoom >= MIN_SAVE_ZOOM;
      },
    };
  };
})();
