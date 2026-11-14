import {test,expect} from '../../web/node_modules/@playwright/test';
test.use({launchOptions:{executablePath:process.env.CHROMIUM_PATH,args:['--no-sandbox','--use-fake-device-for-media-stream','--use-fake-ui-for-media-stream']},permissions:['camera','microphone']});
test('hotplug refresh and ended prompt; choosing an input while off never captures',async({page})=>{
 await page.goto('/login');
 await page.evaluate(async()=>{
  const React=(await import('/node_modules/.vite/deps/react.js')).default;const {createRoot}=(await import('/node_modules/.vite/deps/react-dom_client.js')).default;const {default:DevicePrepare}=await import('/src/components/DevicePrepare.tsx');
  const original=navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);(window as any).deviceCaptured=[];
  navigator.mediaDevices.getUserMedia=async options=>{const stream=await original(options);(window as any).deviceCaptured.push(...stream.getTracks());return stream};
  (window as any).inventory=[{kind:'audioinput',deviceId:'test-mic',groupId:'test',label:'测试麦克风'},{kind:'videoinput',deviceId:'test-cam',groupId:'test',label:'测试摄像头'}];
  navigator.mediaDevices.enumerateDevices=async()=>(window as any).inventory;
  const el=document.createElement('div');document.body.appendChild(el);const root=createRoot(el);(window as any).deviceRoot=root;root.render(React.createElement(DevicePrepare,{onEnter:()=>{}}));
 });
 await expect(page.getByLabel('麦克风设备',{exact:true}).locator('option')).toHaveCount(2);
 await page.getByLabel('麦克风设备',{exact:true}).selectOption('test-mic');await page.getByLabel('摄像头设备',{exact:true}).selectOption('test-cam');
 expect(await page.evaluate(()=>(window as any).deviceCaptured.length)).toBe(0);
 await page.evaluate(()=>{(window as any).inventory=[{kind:'audioinput',deviceId:'test-new',label:'新插入麦克风',groupId:'test'}];navigator.mediaDevices.dispatchEvent(new Event('devicechange'))});
 await expect(page.getByLabel('麦克风设备',{exact:true}).locator('option').filter({hasText:'新插入麦克风'})).toHaveCount(1);
 await page.getByRole('button',{name:'刷新设备列表',exact:true}).click();expect(await page.evaluate(()=>(window as any).deviceCaptured.length)).toBe(0);
 await page.getByLabel('麦克风设备',{exact:true}).selectOption('');await page.getByRole('button',{name:'开启麦克风',exact:true}).click();await expect(page.getByRole('button',{name:'关闭麦克风',exact:true})).toBeVisible();
 await page.evaluate(()=>{const t=(window as any).deviceCaptured.find((t:MediaStreamTrack)=>t.kind==='audio');t.stop();t.dispatchEvent(new Event('ended'))});
 await expect(page.getByRole('alert').filter({hasText:'设备已断开'})).toBeVisible();await expect(page.getByRole('button',{name:'开启麦克风',exact:true})).toBeEnabled();
 await page.evaluate(()=>(window as any).deviceRoot.unmount());expect(await page.evaluate(()=>(window as any).deviceCaptured.every((t:MediaStreamTrack)=>t.readyState==='ended'))).toBe(true);
 // In-room device selection while muted/off uses preferences only, not SDK capture.
 const noCapture=await page.evaluate(async()=>{const{MediaClient}=await import('/src/services/media.ts');let calls=0;const c=new MediaClient('test','test',()=>{});c.room={localParticipant:{isMicrophoneEnabled:false,isCameraEnabled:false},async switchActiveDevice(){calls++;return true}} as any;await c.switchDevice('audioinput','chosen-mic');await c.switchDevice('videoinput','chosen-cam');return calls});expect(noCapture).toBe(0);
});
