const puppeteer = require('puppeteer-core');
(async () => {
  const b = await puppeteer.launch({executablePath:'/usr/bin/google-chrome', args:['--no-sandbox','--allow-file-access-from-files']});
  for (const n of ['naskh','amiri','scheherazade']) {
    const p = await b.newPage();
    await p.setViewport({width:360,height:740,deviceScaleFactor:2,isMobile:true,hasTouch:true});
    const t0=Date.now();
    await p.goto('file://'+__dirname+'/page/'+n+'.html',{waitUntil:'networkidle0'});
    await p.evaluate(()=>document.fonts.ready);
    const sw = await p.evaluate(()=>document.documentElement.scrollWidth);
    console.log(n,'load',Date.now()-t0,'ms scrollWidth',sw);
    await p.screenshot({path:'shot-'+n+'.png',fullPage:true});
    await p.pdf({path:'print-'+n+'.pdf',format:'A4',printBackground:true});
  }
  await b.close();
})();
