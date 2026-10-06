const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const origin = process.env.BASE_URL || 'http://127.0.0.1:18080';
let passed = 0;
const check=(value,label)=>{assert(value,label);passed++;console.log(`[PASS] ${label}`);};
(async()=>{
 const browser=await chromium.launch({headless:true,...(process.env.CHROME_PATH?{executablePath:process.env.CHROME_PATH}:{})});
 try {
  for (const [kind,width,height] of [['desktop',1440,900],['mobile',390,844]]) {
   const context=await browser.newContext({viewport:{width,height},reducedMotion:'reduce'});
   const page=await context.newPage();const errors=[];const policyErrors=[];
   page.on('pageerror',e=>errors.push(e.name));page.on('console',m=>{if(m.type()==='error'&&/content security policy|violates/i.test(m.text()))policyErrors.push(true);});
   await page.goto(origin);await page.waitForFunction(()=>document.querySelector('#data-source').textContent==='DATA SOURCE: REAL');
   check(!(await page.locator('#demo-examples').isVisible()),`${kind}: fixtures absent in live mode`);
   await page.locator('#search-input').fill('https://www.tiktok.com/@marcmarquez93?_r=1&_t=ZS-9AKf9BKqZhS');await page.locator('#search-button').click();
   await page.waitForSelector('.profile-name h2',{timeout:25000});
   check(await page.locator('.profile-name h2').textContent()==='Marc Márquez',`${kind}: actual TikTok identity`);
   check((await page.locator('.profile-username').textContent()).includes('@marcmarquez93'),`${kind}: actual TikTok username`);
   check(await page.locator('.tab').count()===0,`${kind}: unsupported TikTok media tabs absent`);
   await page.locator('.profile-heading .avatar').evaluate(el=>el.decode());
   check(await page.locator('.profile-heading .avatar').evaluate(el=>el.naturalWidth>0&&!el.src.includes('/assets/fixtures/')),`${kind}: real TikTok CDN avatar decoded`);
   await page.screenshot({path:path.resolve(`artifacts/screenshots/${kind}-real-tiktok.png`),fullPage:true});
   await page.locator('#search-input').fill('https://www.instagram.com/marcmarquez93/');await page.locator('#search-button').click();
   await page.waitForSelector('.media-card',{timeout:25000});
   check(await page.locator('#data-source').textContent()==='DATA SOURCE: REAL',`${kind}: explicit REAL provenance`);
   check((await page.locator('.profile-username').textContent()).includes('@marcmarquez93 · Instagram'),`${kind}: URL overrides selected platform`);
   check(await page.locator('.media-card').count()>0,`${kind}: actual public Instagram photo previews`);
   const photos=page.locator('.media-cover img');
   await photos.first().evaluate(el=>el.decode());
   check(await photos.first().evaluate(el=>el.naturalWidth>0&&new URL(el.src).hostname.endsWith('.cdninstagram.com')),`${kind}: actual public CDN photo decoded`);
   check(await page.locator('.download-button').count()===0,`${kind}: unverified downloads absent`);
   check(await page.locator('.tab').count()===1,`${kind}: only verified posts tab`);
   check((await page.locator('.media-type').first().textContent()).includes('preview'),`${kind}: previews labeled accurately`);
   check(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),`${kind}: no horizontal overflow`);
   for (let i=0;i<await photos.count();i++) { await photos.nth(i).scrollIntoViewIfNeeded(); await photos.nth(i).evaluate(el=>el.decode()); }
   check(await photos.evaluateAll(imgs=>imgs.every(el=>el.naturalWidth>0)),`${kind}: every returned real photo decoded`);
   await page.evaluate(()=>scrollTo(0,0));
   await page.screenshot({path:path.resolve(`artifacts/screenshots/${kind}-real-instagram.png`),fullPage:true});
   await page.locator('.media-cover').first().click();await page.waitForSelector('#viewer[open]');
   await page.locator('#viewer-media img').evaluate(el=>el.decode());
   check(await page.locator('#viewer-media img').evaluate(el=>el.naturalWidth>0),`${kind}: real photo renders in viewer`);
   check(!(await page.locator('#viewer-download').isVisible()),`${kind}: unsupported viewer download absent`);
   await page.screenshot({path:path.resolve(`artifacts/screenshots/${kind}-real-media.png`)});
   await page.keyboard.press('Escape');
   await page.locator('#search-input').fill('https://www.facebook.com/bacbeodangiuu');await page.locator('#search-button').click();
   await page.waitForSelector('.state-panel h2');
   check((await page.locator('.state-panel h2').textContent()).includes('unavailable'),`${kind}: Facebook blocker explicit`);
   check(await page.locator('.media-card').count()===0,`${kind}: no mock fallback for Facebook`);
   check(await page.locator('#data-source').textContent()==='DATA SOURCE: REAL',`${kind}: unavailable real source stays explicit`);
   check(errors.length===0,`${kind}: no JavaScript errors`);check(policyErrors.length===0,`${kind}: no CSP violations`);
   await context.close();
  }
  console.log(`${passed} passed\n0 failed`);
 }finally{await browser.close();}
})().catch(e=>{console.error(e.message);process.exitCode=1;});
