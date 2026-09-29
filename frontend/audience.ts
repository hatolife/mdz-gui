interface AudienceState {id:string;slides?:unknown[];theme:string;aspect:string;marginColor:string;index:number;fullscreen:boolean;closed:boolean}
interface AudienceAPI {Poll(withSlides:boolean):Promise<AudienceState>;Control(command:string,index:number):Promise<void>;Close():void;Fullscreen(active:boolean):void}
const api=(window as unknown as {go:{main:{AudienceWindow:AudienceAPI}}}).go.main.AudienceWindow;
const frame=document.getElementById('audience-frame') as HTMLIFrameElement;
const connection=document.getElementById('connection')!;
let data:AudienceState|undefined, frameReady=false, loaded=false, applying=false, fullscreen=false, failures=0, localIndex=-1;
const token=1;
function sendDeck():void {
	if(!frameReady||!data?.slides)return;
	frame.contentWindow?.postMessage({type:'mdz-slide-render',token,...data,presentation:true},location.origin);
}
async function control(command:string,index=0):Promise<void>{try{await api.Control(command,index)}catch(error){connection.textContent=String(error);connection.hidden=false}}
window.addEventListener('message',event=>{
	if(event.source!==frame.contentWindow||event.origin!==location.origin)return;
	const message=event.data;
	if(message?.type==='mdz-slide-ready'){frameReady=true;sendDeck();return}
	if(message?.token!==token)return;
	if(message.type==='mdz-slide-rendered'){loaded=true;connection.hidden=true;frame.focus();void control('ready')}
	if(message.type==='mdz-slide-index' && !applying && message.index!==localIndex){localIndex=message.index;void control('goto',message.index)}
	if(message.type==='mdz-slide-fullscreen')void control('fullscreen');
	if(message.type==='mdz-slide-exit')void control('closed');
});
window.addEventListener('keydown',event=>{
	if(event.key.toLowerCase()==='f'){event.preventDefault();void control('fullscreen')}
	if(event.key==='Escape'){event.preventDefault();void control('closed')}
});
async function poll():Promise<void>{
	try{
		const next=await api.Poll(!data);failures=0;
		if(next.closed){api.Close();return}
		if(!data){data=next;localIndex=next.index;sendDeck()}
		if(next.fullscreen!==fullscreen){fullscreen=next.fullscreen;api.Fullscreen(fullscreen)}
		if(loaded&&next.index!==localIndex){
			localIndex=next.index;applying=true;
			frame.contentWindow?.postMessage({type:'mdz-slide-command',command:'goto',index:next.index},location.origin);
			// 同じ番号の通知は送り返さず、投影側の操作だけを反映します。
			applying=false;
		}
	}catch(error){if(++failures>=3){connection.textContent='発表者側との接続が終了しました';connection.hidden=false;api.Close();return}}
	window.setTimeout(()=>void poll(),150);
}
void poll();
export {};

window.addEventListener('contextmenu',event=>{event.preventDefault();void control('closed')});
