package exam

import (
	"regexp"
	"strconv"
)

// Cek perangkat peserta (T136). Matriks dukungan mengikuti standar siap
// produksi MVP 4 bagian 4.5: Chrome Android ≥ 90, Safari iOS ≥ 15,
// Chrome/Edge desktop dua tahun terakhir, Firefox ESR.

// Hasil cek perangkat.
const (
	DeviceOK   = "ok"   // siap
	DeviceWarn = "warn" // bisa, dengan saran
	DeviceFail = "fail" // tidak didukung
)

// Versi minimum.
const (
	minChromeAndroid = 90
	minChromeDesktop = 110
	minEdge          = 110
	minFirefox       = 115
	minSafariIOS     = 15
	minSafariMac     = 15
	minSamsung       = 15
)

// DeviceVerdict adalah hasil penilaian browser dari User-Agent.
type DeviceVerdict struct {
	Level   string
	Browser string
	Message string
}

var (
	reChrome   = regexp.MustCompile(`(?:Chrome|CriOS)/(\d+)`)
	reEdge     = regexp.MustCompile(`Edg(?:A|iOS)?/(\d+)`)
	reFirefox  = regexp.MustCompile(`(?:Firefox|FxiOS)/(\d+)`)
	reSamsung  = regexp.MustCompile(`SamsungBrowser/(\d+)`)
	reIOS      = regexp.MustCompile(`OS (\d+)_\d+(?:_\d+)? like Mac OS X`)
	reSafari   = regexp.MustCompile(`Version/(\d+)[.\d]* (?:Mobile/\S+ )?Safari`)
	reInApp    = regexp.MustCompile(`FBAN|FBAV|FB_IAB|Instagram|Line/|WhatsApp|MicroMessenger|TikTok|; wv\)`)
	reOpMini   = regexp.MustCompile(`Opera Mini|OPiOS.*Mini`)
	reUC       = regexp.MustCompile(`UCBrowser|UCWEB`)
	reIE       = regexp.MustCompile(`MSIE |Trident/`)
	reAndroid  = regexp.MustCompile(`Android`)
	reIPhone   = regexp.MustCompile(`iPhone|iPad|iPod`)
	reOldStock = regexp.MustCompile(`Android [0-6]\.`)
)

func ver(re *regexp.Regexp, ua string) int {
	m := re.FindStringSubmatch(ua)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// ClassifyUA menilai browser peserta dari User-Agent. Pemeriksaan fitur
// (service worker, layar penuh, suara, rumus) dilakukan di browser.
func ClassifyUA(ua string) DeviceVerdict {
	switch {
	case ua == "":
		return DeviceVerdict{Level: DeviceWarn, Browser: "Tidak dikenal", Message: "Browser tidak dikenali. Gunakan Chrome versi terbaru."}
	case reIE.MatchString(ua):
		return DeviceVerdict{Level: DeviceFail, Browser: "Internet Explorer", Message: "Internet Explorer tidak didukung. Gunakan Chrome atau Edge."}
	case reOpMini.MatchString(ua):
		return DeviceVerdict{Level: DeviceFail, Browser: "Opera Mini", Message: "Opera Mini tidak bisa menjalankan ujian. Gunakan Chrome."}
	case reUC.MatchString(ua):
		return DeviceVerdict{Level: DeviceFail, Browser: "UC Browser", Message: "UC Browser tidak didukung. Gunakan Chrome."}
	case reOldStock.MatchString(ua):
		return DeviceVerdict{Level: DeviceFail, Browser: "Android lama", Message: "Versi Android terlalu lama. Gunakan HP lain atau komputer lab."}
	case reInApp.MatchString(ua):
		return DeviceVerdict{Level: DeviceWarn, Browser: "Browser dalam aplikasi", Message: "Buka tautan ini di Chrome, bukan dari dalam WhatsApp/Instagram/Facebook."}
	}
	if reIPhone.MatchString(ua) {
		// Semua browser iOS memakai mesin Safari; versi iOS yang menentukan.
		v := ver(reIOS, ua)
		if v > 0 && v < minSafariIOS {
			return DeviceVerdict{Level: DeviceFail, Browser: "Safari iOS " + strconv.Itoa(v), Message: "iOS terlalu lama. Perbarui ke iOS 15 atau lebih baru."}
		}
		return DeviceVerdict{Level: DeviceOK, Browser: "Safari iOS", Message: "Siap dipakai."}
	}
	if v := ver(reEdge, ua); v > 0 {
		if v < minEdge {
			return DeviceVerdict{Level: DeviceWarn, Browser: "Edge " + strconv.Itoa(v), Message: "Perbarui Edge ke versi terbaru."}
		}
		return DeviceVerdict{Level: DeviceOK, Browser: "Edge " + strconv.Itoa(v), Message: "Siap dipakai."}
	}
	if v := ver(reSamsung, ua); v > 0 {
		if v < minSamsung {
			return DeviceVerdict{Level: DeviceWarn, Browser: "Samsung Internet " + strconv.Itoa(v), Message: "Gunakan Chrome agar lebih stabil."}
		}
		return DeviceVerdict{Level: DeviceWarn, Browser: "Samsung Internet " + strconv.Itoa(v), Message: "Bisa dipakai, tetapi Chrome lebih disarankan."}
	}
	if v := ver(reFirefox, ua); v > 0 {
		if v < minFirefox {
			return DeviceVerdict{Level: DeviceWarn, Browser: "Firefox " + strconv.Itoa(v), Message: "Perbarui Firefox atau gunakan Chrome."}
		}
		return DeviceVerdict{Level: DeviceOK, Browser: "Firefox " + strconv.Itoa(v), Message: "Siap dipakai."}
	}
	if v := ver(reChrome, ua); v > 0 {
		min := minChromeDesktop
		if reAndroid.MatchString(ua) {
			min = minChromeAndroid
		}
		if v < min {
			level := DeviceWarn
			if v < 70 {
				level = DeviceFail
			}
			return DeviceVerdict{Level: level, Browser: "Chrome " + strconv.Itoa(v), Message: "Chrome terlalu lama. Perbarui lewat Play Store atau situs Google."}
		}
		return DeviceVerdict{Level: DeviceOK, Browser: "Chrome " + strconv.Itoa(v), Message: "Siap dipakai."}
	}
	if v := ver(reSafari, ua); v > 0 {
		if v < minSafariMac {
			return DeviceVerdict{Level: DeviceWarn, Browser: "Safari " + strconv.Itoa(v), Message: "Perbarui Safari atau gunakan Chrome."}
		}
		return DeviceVerdict{Level: DeviceOK, Browser: "Safari " + strconv.Itoa(v), Message: "Siap dipakai."}
	}
	if reAndroid.MatchString(ua) {
		return DeviceVerdict{Level: DeviceWarn, Browser: "Browser bawaan", Message: "Gunakan Chrome, bukan browser bawaan HP."}
	}
	return DeviceVerdict{Level: DeviceWarn, Browser: "Tidak dikenal", Message: "Browser tidak dikenali. Gunakan Chrome versi terbaru."}
}
