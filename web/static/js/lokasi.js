// Titik lokasi rumah (T106, T112): tombol "Ambil lokasi saya" (GPS
// perangkat) dan "Pilih di peta". Penyedia peta: Google Maps jika
// GOOGLE_MAPS_API_KEY diset, selain itu OpenStreetMap (Leaflet yang
// disertakan di /static/vendor/leaflet, ubin dari tile.openstreetmap.org).
// Skrip dan ubin peta baru dimuat saat tombol peta ditekan, jadi tidak ada
// permintaan ke penyedia peta selama orang tua tidak membuka peta. Yang
// dilihat penyedia hanya tampilan peta; nama dan isian lain tidak dikirim.
// Validasi koordinat tetap di server.
(function () {
  const INDONESIA = { lat: -2.5, lng: 118, zoom: 5 };
  const MIN_SAVE_ZOOM = 16;
  const MSG_LOAD = 'Peta gagal dimuat. Periksa koneksi internet lalu coba lagi, atau gunakan "Ambil lokasi saya".';
  const MSG_DENIED = 'Peta tidak bisa dibuka saat ini (layanan peta menolak permintaan). Gunakan "Ambil lokasi saya" atau kosongkan.';
  const once = {};

  // loadOnce menjalankan pemuat sekali per halaman; gagal boleh dicoba lagi.
  function loadOnce(name, fn) {
    if (!once[name]) {
      once[name] = fn().catch((e) => {
        once[name] = null;
        throw e;
      });
    }
    return once[name];
  }

  function addScript(src, attrs) {
    return new Promise((resolve, reject) => {
      const s = document.createElement('script');
      s.src = src;
      s.async = true;
      Object.assign(s, attrs || {});
      s.onload = () => resolve();
      s.onerror = () => reject(new Error('load'));
      document.head.appendChild(s);
    });
  }

  // ---------- Google Maps ----------
  function loadGoogle(key) {
    if (window.google && window.google.maps && window.google.maps.importLibrary) return Promise.resolve();
    return loadOnce('google', () => new Promise((resolve, reject) => {
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
      addScript('https://maps.googleapis.com/maps/api/js?key=' + encodeURIComponent(key) +
        '&loading=async&callback=__akraMapsReady&language=id&region=ID&v=weekly').catch((e) => {
        clearTimeout(timer);
        reject(e);
      });
    }));
  }

  async function createGoogle(container, start, opts) {
    await loadGoogle(opts.key);
    const { Map } = await google.maps.importLibrary('maps');
    const map = new Map(container, {
      center: { lat: start.lat, lng: start.lng },
      zoom: start.zoom,
      mapTypeControl: true,
      mapTypeControlOptions: { mapTypeIds: ['roadmap', 'hybrid'] },
      streetViewControl: false,
      fullscreenControl: false,
      clickableIcons: false,
      gestureHandling: 'greedy',
    });
    // Mengetuk peta memindahkan titik ke tempat yang diketuk.
    map.addListener('click', (e) => map.panTo(e.latLng));
    return {
      center: () => ({ lat: map.getCenter().lat(), lng: map.getCenter().lng() }),
      zoom: () => map.getZoom(),
      setView: (p) => {
        map.setCenter({ lat: p.lat, lng: p.lng });
        map.setZoom(p.zoom);
      },
      onZoom: (cb) => map.addListener('zoom_changed', cb),
      resize: () => {},
    };
  }

  // ---------- OpenStreetMap (Leaflet) ----------
  function loadLeaflet(js, css) {
    if (window.L && window.L.map) return Promise.resolve();
    return loadOnce('leaflet', () => {
      if (!document.querySelector('link[data-leaflet]')) {
        const l = document.createElement('link');
        l.rel = 'stylesheet';
        l.href = css;
        l.dataset.leaflet = '1';
        document.head.appendChild(l);
      }
      return addScript(js);
    });
  }

  async function createOSM(container, start, opts) {
    await loadLeaflet(opts.leafletJS, opts.leafletCSS);
    const map = L.map(container, { zoomControl: true }).setView([start.lat, start.lng], start.zoom);
    const tiles = L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      maxZoom: 19,
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">OpenStreetMap</a>',
    }).addTo(map);
    let loaded = 0;
    let failed = 0;
    tiles.on('tileload', () => loaded++);
    tiles.on('tileerror', () => {
      failed++;
      if (!loaded && failed >= 4 && opts.onError) opts.onError(MSG_LOAD);
    });
    map.on('click', (e) => map.panTo(e.latlng));
    return {
      center: () => ({ lat: map.getCenter().lat, lng: map.getCenter().lng }),
      zoom: () => map.getZoom(),
      setView: (p) => map.setView([p.lat, p.lng], p.zoom),
      onZoom: (cb) => map.on('zoomend', cb),
      resize: () => map.invalidateSize(),
    };
  }

  // ---------- Pusat awal dari isian wilayah (T110) ----------
  async function wilayahList(induk) {
    const cache = (window.__wilayahCache = window.__wilayahCache || {});
    if (cache[induk]) return cache[induk];
    const res = await fetch('/wilayah?induk=' + encodeURIComponent(induk), { headers: { Accept: 'application/json' } });
    if (!res.ok) throw new Error('wilayah ' + res.status);
    cache[induk] = await res.json();
    return cache[induk];
  }

  // regionCenter mencari titik kabupaten/kota (atau provinsi) dari isian
  // wilayah di form yang sama.
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
    const provider = el.dataset.mapProvider || 'osm';
    return {
      lat: el.dataset.lat || '',
      lng: el.dataset.lng || '',
      state: '',
      msg: '',
      can: window.isSecureContext && 'geolocation' in navigator,
      mapOpen: false,
      mapLoading: false,
      mapError: '',
      zoom: 0,
      map: null,
      init() {
        // Kunci Google ditolak setelah skrip termuat (salah, referrer, kuota).
        window.addEventListener('akra-maps-auth-failure', () => {
          this.mapError = MSG_DENIED;
          this.mapLoading = false;
        });
      },
      get viewUrl() {
        return 'https://www.openstreetmap.org/?mlat=' + encodeURIComponent(this.lat) + '&mlon=' + encodeURIComponent(this.lng) +
          '#map=18/' + encodeURIComponent(this.lat) + '/' + encodeURIComponent(this.lng);
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
        await this.$nextTick();
        if (this.map) {
          this.map.resize();
          this.map.setView(start);
          return;
        }
        this.mapLoading = true;
        try {
          const create = provider === 'google' ? createGoogle : createOSM;
          this.map = await create(el.querySelector('.lokasi-map'), start, {
            key: el.dataset.mapsKey || '',
            leafletJS: el.dataset.leafletJs,
            leafletCSS: el.dataset.leafletCss,
            onError: (m) => (this.mapError = m),
          });
          this.zoom = this.map.zoom();
          this.map.onZoom(() => (this.zoom = this.map.zoom()));
          this.map.resize();
        } catch (e) {
          this.mapError = e && e.message === 'auth' ? MSG_DENIED : MSG_LOAD;
        } finally {
          this.mapLoading = false;
        }
      },
      closeMap() {
        this.mapOpen = false;
        document.body.style.overflow = '';
      },
      useMapPoint() {
        if (!this.canSave) return;
        const c = this.map.center();
        this.lat = c.lat.toFixed(6);
        this.lng = c.lng.toFixed(6);
        this.state = 'ok';
        this.msg = 'Titik rumah dipilih di peta.';
        this.closeMap();
      },
      get canSave() {
        return !!this.map && !this.mapError && this.zoom >= MIN_SAVE_ZOOM;
      },
    };
  };
})();
