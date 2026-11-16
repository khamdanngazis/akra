const puppeteer = require('puppeteer-core');
(async () => {
  const b = await puppeteer.launch({executablePath:'/usr/bin/google-chrome', args:['--no-sandbox']});
  const p = await b.newPage();
  await p.setViewport({width:794,height:1123});
  await p.emulateMediaType('print');
  await p.goto('file://'+__dirname+'/page/naskh.html',{waitUntil:'networkidle0'});
  await p.screenshot({path:'printmedia-naskh.png',fullPage:true});
  await b.close();
})();
