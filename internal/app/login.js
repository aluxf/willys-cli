'use strict';
const message=document.getElementById('message'),qr=document.getElementById('qr'),cancel=document.getElementById('cancel');
let stopped=false;
async function update(){
 if(stopped)return;
 try{
  const response=await fetch('status',{cache:'no-store'});if(!response.ok)throw new Error();
  const data=await response.json();message.textContent=data.message;
  if(data.state!=='pending'){stopped=true;qr.hidden=true;cancel.hidden=true;return;}
  if(data.qrReady){qr.src=`qr?t=${Date.now()}`;qr.hidden=false;}else{qr.hidden=true;}
 }catch{message.textContent='The login window closed. Check the CLI for the result.';qr.hidden=true;cancel.hidden=true;stopped=true;}
 if(!stopped)setTimeout(update,1000);
}
cancel.addEventListener('click',async()=>{stopped=true;cancel.disabled=true;qr.hidden=true;try{await fetch('cancel',{method:'POST'});message.textContent='Login canceled.';}catch{message.textContent='Check the CLI to confirm cancellation.';}});
update();
