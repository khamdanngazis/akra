const katex = require('katex'); const fs = require('fs');
const m = s => katex.renderToString(s, {throwOnError:true});
const fonts = {naskh:'noto-naskh-arabic-arabic-400-normal.woff2', amiri:'amiri-arabic-400-normal.woff2', scheherazade:'scheherazade-new-arabic-400-normal.woff2'};
for (const [name, file] of Object.entries(fonts)) {
const html = `<!doctype html><html lang="id"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<link rel="stylesheet" href="katex.min.css">
<style>
@font-face{font-family:Arab;src:url(fonts/${file}) format("woff2");unicode-range:U+0600-06FF,U+0750-077F,U+08A0-08FF,U+FB50-FDFF,U+FE70-FEFF;}
body{font-family:system-ui,sans-serif;font-size:16px;margin:0;padding:16px;line-height:1.6}
[lang=ar],.ar{font-family:Arab,serif;font-size:1.5em;line-height:2.1}
.q{display:grid;grid-template-columns:2.2em 1fr;gap:.25em;margin-bottom:1.2em}
.opts{list-style:none;padding:0;margin:.5em 0 0}
.opts li{display:grid;grid-template-columns:2em 1fr;align-items:baseline;border:1px solid #ccc;border-radius:8px;padding:.4em .6em;margin:.3em 0}
.opts b{direction:ltr}
.katex{direction:ltr;unicode-bidi:isolate}
@media print{body{padding:0}.q{break-inside:avoid}}
</style></head><body>
<h3>Font: ${name}</h3>
<div class="q"><b>1.</b><div><p dir="auto">Terjemahkan kalimat berikut ke dalam bahasa Indonesia:</p>
<p lang="ar" dir="rtl">ذَهَبَ أَحْمَدُ إِلَى الْمَدْرَسَةِ صَبَاحًا، وَقَرَأَ الْقُرْآنَ الْكَرِيمَ.</p>
<ol class="opts"><li><b>A.</b><span dir="auto">Ahmad pergi ke sekolah pada pagi hari, dan membaca Al-Qur'an.</span></li>
<li><b>B.</b><span dir="auto">Ahmad pulang dari masjid pada sore hari.</span></li></ol></div></div>
<div class="q"><b>2.</b><div><p>Arti kata <span lang="ar" dir="rtl">بِسْمِ اللَّهِ الرَّحْمَٰنِ الرَّحِيمِ</span> (ayat 1) adalah …</p>
<ol class="opts"><li><b>A.</b><span lang="ar" dir="rtl">الْحَمْدُ لِلَّهِ رَبِّ الْعَالَمِينَ</span></li>
<li><b>B.</b><span lang="ar" dir="rtl">مَالِكِ يَوْمِ الدِّينِ (٤)</span></li>
<li><b>C.</b><span dir="auto">إِيَّاكَ نَعْبُدُ 123 – campuran</span></li></ol></div></div>
<div class="q"><b>3.</b><div><p>Akar persamaan ${m('x^2-5x+6=0')} adalah … Gunakan ${m('x=\\frac{-b\\pm\\sqrt{b^2-4ac}}{2a}')}.</p>
<ol class="opts"><li><b>A.</b><span>${m('x_1=2,\;x_2=3')}</span></li><li><b>B.</b><span>${m('\\begin{pmatrix}1&2\\\\3&4\\end{pmatrix}')}</span></li>
<li><b>C.</b><span>${m('\\int_0^1 x^2\\,dx=\\tfrac13')}</span></li></ol></div></div>
<div class="q"><b>4.</b><div><p lang="ar" dir="rtl">إِذَا كَانَ ${m('x+2=5')} فَمَا قِيمَةُ ${m('x')}؟</p></div></div>
</body></html>`;
fs.writeFileSync(`${__dirname}/page/${name}.html`, html);
}
