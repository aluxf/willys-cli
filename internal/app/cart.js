'use strict';
let displayedCart = null;
const $ = id => document.getElementById(id);
function element(tag, cls, value) { const node = document.createElement(tag); if (cls) node.className = cls; if (value != null) node.textContent = String(value); return node; }
function httpsURL(raw) { try { const u = new URL(raw); return u.protocol === 'https:' && !u.username && !u.password ? u.href : null; } catch { return null; } }
function money(value) { return typeof value === 'object' && value ? value.formattedValue || value.value || '—' : value || '—'; }
function disablePayment() { $('payment').removeAttribute('href'); $('payment').classList.add('disabled'); $('payment').setAttribute('aria-disabled', 'true'); }
function product(p) {
 const row = element('article', 'product'); const picture = element('div', 'image-wrap');
 const source = httpsURL(typeof p.image === 'object' ? p.image?.url : p.image);
 if (source) { const img = element('img'); img.src = source; img.alt = p.image?.altText || p.name || ''; img.loading = 'lazy'; img.referrerPolicy = 'no-referrer'; img.addEventListener('error', () => { img.remove(); picture.prepend(element('span','missing','No image')); }); picture.append(img); }
 else picture.append(element('span','missing','No image'));
 const info = element('div'); info.append(element('p','brand',p.brand || p.manufacturer));
 const name = element('h2'); name.title = p.name || 'Product'; const link = httpsURL(p.url);
 if (link) { const a = element('a','',p.name); a.href = link; a.target = '_blank'; a.rel = 'noopener noreferrer'; name.append(a); } else name.textContent = p.name || 'Product';
 info.append(name, element('div','meta',`${p.packSize || ''} · ${money(p.price)} each`));
 const quantity = element('span','quantity',`X${p.quantity ?? 0}`); quantity.setAttribute('aria-label',`Quantity ${p.quantity ?? 0}`); picture.append(quantity);
 const price = element('div','price',money(p.lineTotal || p.totalDiscountedPriceWithDeposit));
 if (p.discount && !/^0([,.]0+)?\s/.test(String(p.discount))) price.append(element('div','discount',`Saved ${money(p.discount)}`));
 row.append(picture,info,price);
 if (p.ingredients || p.nutritionFacts || p.nutritionsFactList?.length) {
  const details=element('details'); details.append(element('summary','','Ingredients & nutrition'));
  if(p.ingredients) details.append(element('p','',p.ingredients));
  if(p.nutritionDescription) details.append(element('p','',`Nutrition per ${p.nutritionDescription}`));
  if(p.nutritionsFactList?.length) for(const n of p.nutritionsFactList) details.append(element('div','',`${n.typeCode || ''}: ${n.value || ''} ${n.unitCode || ''}`));
  else if(p.nutritionFacts) details.append(element('p','',p.nutritionFacts));
  row.append(details);
 }
 return row;
}
function sum(label,value,cls='') { const row=element('div',`sum-row ${cls}`); row.append(element('span','',label),element('span','',money(value))); return row; }
function render(data) {
 displayedCart=data;
 const products=data.products || [];
 $('caption').textContent=`${products.length} ${products.length===1?'product':'products'} · ${data.totalUnits || 0} items`;
 $('products').replaceChildren(...products.map(product));
 if(!products.length) $('products').append(element('p','loading','Your cart is empty. Add products with the CLI, then refresh.'));
 const totals=$('totals'); totals.replaceChildren();
 if(data.subtotal) totals.append(sum('Products',data.subtotal));
 totals.append(sum(data.fulfillment==='homeDelivery'?'Delivery':'Pickup',data.deliveryFee),sum('Total',data.total,'total'));
 totals.append(element('p','reservation',`Payment reservation ${money(data.reservation)}. Includes a ${money(data.buffer)} buffer for substitutions and weight changes.`));
 const pickup=data.fulfillment==='pickUpInStore'||data.fulfillment==='pickupInStore';
 $('fulfillment-label').textContent=pickup?'Pickup':'Home delivery';
 $('store').textContent=pickup?(data.store?.name || 'Store not selected'):'Delivery address';
 const address=pickup?(data.store?.address || {}):(data.address || {});
 $('address').textContent=address.formattedAddress || [address.line1 || address.addressLine1, address.postalCode || address.postcode, address.town].filter(Boolean).join(', ');
 $('slot').textContent=data.slot || 'Select a time with willys slots.';
 $('customer').textContent=[data.address?.firstName,data.address?.lastName].filter(Boolean).join(' ');
 disablePayment();
 const payment=httpsURL(data.paymentURL);
 if(payment) { const host=new URL(payment).hostname; if(host==='ecom.payex.com'||host==='payments.klarna.com') { $('payment').href=payment; $('payment').rel='noreferrer'; $('payment').classList.remove('disabled'); $('payment').setAttribute('aria-disabled','false'); } }
 $('payment').textContent = data.paymentCanceled ? 'Start new payment ↗' : 'Continue to payment ↗';
 if(data.paymentCanceled) { $('payment').href='#'; $('payment').classList.remove('disabled'); $('payment').setAttribute('aria-disabled','false'); }
 $('payment-note').textContent=data.paymentMessage || '';
 $('error').hidden=data.cartValidation?.passed !== false;
 if(data.cartValidation?.passed===false) $('error').textContent=data.cartValidation.reason;
 $('updated').textContent=`Updated ${new Date().toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'})}`;
}
async function refresh() {
 $('refresh').disabled=true; $('layout').setAttribute('aria-busy','true'); disablePayment();
 try { const response=await fetch('data',{cache:'no-store'}); const data=await response.json(); if(!response.ok) throw new Error(data.error || 'Cannot load the cart.'); render(data); }
 catch(error) { $('error').hidden=false; $('error').textContent=`Cannot refresh your cart. ${error.message} Keep the CLI command running and try again.`; $('payment-note').textContent='Refresh the cart before continuing to payment.'; }
 finally { $('refresh').disabled=false; $('layout').setAttribute('aria-busy','false'); }
}
$('refresh').addEventListener('click',refresh);
refresh();

$('payment').addEventListener('click',async event=>{
 event.preventDefault();
 if(!displayedCart?.paymentCanceled){
  await refresh();
  if(!displayedCart?.paymentCanceled && $('payment').getAttribute('aria-disabled')==='false') window.location.assign($('payment').href);
  return;
 }
 disablePayment();$('refresh').disabled=true;
 try{
  const response=await fetch('payment',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({total:displayedCart.total,reservation:displayedCart.reservation})});
  const result=await response.json();if(!response.ok)throw new Error(result.error||'Could not start payment.');
  const target=httpsURL(result.url);if(!target||!['ecom.payex.com','payments.klarna.com'].includes(new URL(target).hostname))throw new Error('Unsupported payment link.');
  window.location.assign(target);
 }catch(error){$('error').hidden=false;$('error').textContent=error.message;$('payment-note').textContent='Refresh to check payment status. Do not retry an uncertain payment.';$('refresh').disabled=false;}
});
