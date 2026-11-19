package exam

import "testing"

func TestClassifyUA(t *testing.T) {
	cases := []struct {
		ua, level string
	}{
		{"Mozilla/5.0 (Linux; Android 11; SM-A125F) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.6099.144 Mobile Safari/537.36", DeviceOK},
		{"Mozilla/5.0 (Linux; Android 10; Redmi 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/85.0.4183.127 Mobile Safari/537.36", DeviceWarn},
		{"Mozilla/5.0 (Linux; Android 9; vivo 1820) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/62.0.3202.84 Mobile Safari/537.36", DeviceFail},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1", DeviceOK},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 14_8 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.1.2 Mobile/15E148 Safari/604.1", DeviceFail},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0", DeviceOK},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:128.0) Gecko/20100101 Firefox/128.0", DeviceOK},
		{"Mozilla/5.0 (Windows NT 6.1; Trident/7.0; rv:11.0) like Gecko", DeviceFail},
		{"Opera/9.80 (Android; Opera Mini/36.2.2254/119.132; U; id) Presto/2.12.423 Version/12.16", DeviceFail},
		{"Mozilla/5.0 (Linux; U; Android 8.1.0; en-US; Redmi 5A) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/57.0.2987.108 UCBrowser/13.4.0.1306 Mobile Safari/537.36", DeviceFail},
		{"Mozilla/5.0 (Linux; Android 12; SM-A325F Build/SP1A.210812.016; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/118.0.5993.80 Mobile Safari/537.36 [FB_IAB/FB4A;FBAV/439.0.0.29.120;]", DeviceWarn},
		{"Mozilla/5.0 (Linux; Android 13; SM-A145F) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/23.0 Chrome/115.0.0.0 Mobile Safari/537.36", DeviceWarn},
		{"Mozilla/5.0 (Linux; Android 5.1; Lenovo A6000) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/39.0.0.0 Mobile Safari/537.36", DeviceFail},
		{"", DeviceWarn},
		{"Mozilla/5.0 (Linux; U; Android 12; id-id; Redmi Note 11 Build/SKQ1) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/112.0.5615.136 Mobile Safari/537.36 XiaoMi/MiuiBrowser/14.5.11-gn", DeviceWarn},
		{"Mozilla/5.0 (Linux; Android 13; CPH2387) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Mobile Safari/537.36 OPR/78.1.4143.75935", DeviceWarn},
	}
	for _, c := range cases {
		if got := ClassifyUA(c.ua); got.Level != c.level {
			t.Errorf("%q = %s (%s), want %s", c.ua, got.Level, got.Browser, c.level)
		}
	}
	if v := ClassifyUA(cases[10].ua); v.Message == "" || v.Browser != "Browser dalam aplikasi" {
		t.Errorf("in-app = %+v", v)
	}
}
