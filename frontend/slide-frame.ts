/// <reference path="./reveal.d.ts" />
import Reveal from './vendor/reveal.esm.js';
interface FrameSlide { id: string; file: string; title: string; layout: string; fontSize: number; background: string; html: string }
interface FrameData { type: string; token: number; slides: FrameSlide[]; theme: string; aspect: string; index: number; presentation: boolean; editorPreview?: boolean; previewNavigation?: boolean; debugOutline?: boolean; marginColor?: string; contentMarginX?: number; contentMarginY?: number; fontFamily?: string; bodyFontSize?: number; h1FontSize?: number; h2FontSize?: number; h3FontSize?: number; h4FontSize?: number; h5FontSize?: number }
let deck: Reveal | undefined;
let activeToken = 0;
let currentData: FrameData | undefined;
let queue = Promise.resolve();
const send = (value: Record<string, unknown>): void => parent.postMessage({...value, token: activeToken}, location.origin);
function fontStack(value?: string): string | undefined {
	switch (value) {
	case 'system': return 'Segoe UI, Yu Gothic UI, Yu Gothic, Meiryo, sans-serif';
	case 'gothic': return 'Yu Gothic UI, Yu Gothic, Meiryo, sans-serif';
	case 'mincho': return 'Yu Mincho, Hiragino Mincho ProN, Noto Serif JP, serif';
	case 'monospace': return 'Cascadia Mono, Consolas, monospace';
	default: return undefined;
	}
}
function applyTypography(section: HTMLElement, data: FrameData, slide: FrameSlide): void {
	section.style.setProperty('--slide-margin-x', `${data.contentMarginX || 60}px`);
	section.style.setProperty('--slide-margin-y', `${data.contentMarginY || 48}px`);
	section.style.fontSize = `${data.bodyFontSize || slide.fontSize}px`;
	const family = fontStack(data.fontFamily);
	if (family) section.style.fontFamily = family;
	for (const level of [1,2,3,4,5]) {
		const size = data[`h${level}FontSize` as keyof FrameData];
		if (typeof size === 'number' && size > 0) section.style.setProperty(`--slide-h${level}-size`, `${size}px`);
	}
}
function checkOverflow(): void {
	const overflows: string[] = [];
	for (const section of document.querySelectorAll<HTMLElement>('.slides>section')) {
		if (section.style.display === 'none') continue;
		const content = section.querySelector<HTMLElement>('.slide-content')!;
		const bounds = content.getBoundingClientRect();
		const exceeds = (rect: DOMRect): boolean => rect.width > 0 && rect.height > 0 &&
			(rect.top < bounds.top - 2 || rect.left < bounds.left - 2 || rect.bottom > bounds.bottom + 2 || rect.right > bounds.right + 2);
		let overflow = false;
		const walker = document.createTreeWalker(content, NodeFilter.SHOW_TEXT);
		for (let node = walker.nextNode(); node && !overflow; node = walker.nextNode()) {
			if (!node.textContent?.trim()) continue;
			const range = document.createRange();
			range.selectNodeContents(node);
			overflow = [...range.getClientRects()].some(rect => exceeds(rect));
			range.detach();
		}
		if (!overflow) {
			for (const node of content.querySelectorAll<HTMLElement>('img,svg,canvas,video,table,hr')) {
				if (exceeds(node.getBoundingClientRect())) { overflow = true; break; }
			}
		}
		if (overflow) overflows.push(section.dataset.id!);
	}
	send({type:'mdz-slide-overflow', ids:overflows});
}
async function display(data: FrameData): Promise<void> {
	deck?.destroy();
	activeToken = data.token; currentData = data;
	document.documentElement.classList.toggle('editor-preview', !!data.editorPreview);
	document.documentElement.classList.toggle('debug-outline', !!data.editorPreview && !!data.debugOutline);
	document.body.style.backgroundColor = /^#[0-9a-f]{6}$/i.test(data.marginColor||'') ? data.marginColor! : '#ffffff';
	document.documentElement.style.backgroundColor = document.body.style.backgroundColor;
	const root = document.createElement('div'); root.className = 'reveal';root.style.backgroundColor=document.body.style.backgroundColor;
	const slides = document.createElement('div'); slides.className = 'slides'; root.append(slides);
	document.body.replaceChildren(root);
	for (const slide of data.slides) {
		const section = document.createElement('section');
		section.dataset.id = slide.id; section.dataset.theme = data.theme; section.dataset.layout = slide.layout;
		applyTypography(section, data, slide);
		if (/^#[0-9a-f]{6}$/i.test(slide.background || '')) section.style.backgroundColor = slide.background;
		const content = document.createElement('div'); content.className = 'slide-content';
		// 本文はバックエンドのGoldmarkで生HTMLを禁止して変換済みです。
		content.innerHTML = slide.html; section.append(content); slides.append(section);
		for (const image of content.querySelectorAll('img')) image.addEventListener('load', checkOverflow);
	}
	deck = new Reveal(root, {width:960, height:data.aspect==='4:3'?720:540, margin:data.presentation?0:.04, minScale:.05, maxScale:10,
		embedded:true, center:false, controls:false, progress:false, slideNumber:false,
		keyboard:false, touch:data.presentation, transition:'none', backgroundTransition:'none', hash:false, history:false,
		view:'slide', scrollActivationWidth:null, disableLayout:false, overview:data.presentation});
	await deck.initialize();
	deck.slide(data.index);
	deck.on('slidechanged', () => { send({type:'mdz-slide-index', index:deck!.getIndices().h}); checkOverflow(); });
	deck.on('overviewhidden', () => send({type:'mdz-slide-index', index:deck!.getIndices().h}));
	requestAnimationFrame(checkOverflow);
	send({type:'mdz-slide-rendered'});
}
window.addEventListener('message', event => {
	if (event.source!==parent || event.origin!==location.origin) return;
	if (event.data?.type==='mdz-slide-render') queue = queue.then(()=>display(event.data)).catch(error=>send({type:'mdz-slide-error', message:String(error)}));
	if (event.data?.type==='mdz-slide-command') {
		if (event.data.command==='next') deck?.next();
		if (event.data.command==='prev') deck?.prev();
		if (event.data.command==='overview') deck?.toggleOverview();
		if (event.data.command==='first') deck?.slide(0);
		if (event.data.command==='last') deck?.slide((currentData?.slides.length || 1)-1);
		if (event.data.command==='goto' && Number.isInteger(event.data.index)) deck?.slide(event.data.index);
	}
});
window.addEventListener('keydown', event => {
	if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase()==='s') { event.preventDefault(); send({type:'mdz-slide-save', saveAs:event.shiftKey}); return; }
	if (!currentData?.presentation || event.ctrlKey || event.metaKey || event.altKey) return;
	if (event.key==='Enter') { event.preventDefault(); if(event.shiftKey) deck?.prev(); else deck?.next(); }
	if (['ArrowRight','ArrowDown','PageDown',' '].includes(event.key)) { event.preventDefault(); deck?.next(); }
	if (['ArrowLeft','ArrowUp','PageUp'].includes(event.key)) { event.preventDefault(); deck?.prev(); }
	if (event.key==='Home') { event.preventDefault(); deck?.slide(0); }
	if (event.key==='End') { event.preventDefault(); deck?.slide(currentData.slides.length-1); }
	if (event.key.toLowerCase()==='f') { event.preventDefault(); send({type:'mdz-slide-fullscreen'}); }
	if (event.key==='Escape') { event.preventDefault(); send({type:'mdz-slide-exit'}); }
	if (event.key.toLowerCase()==='o') { event.preventDefault(); deck?.toggleOverview(); }
});
// ホイールの各入力を1ページの移動として扱います。
document.addEventListener('wheel', event => {
	if (event.ctrlKey || document.querySelector('.reveal.overview') || !event.deltaY) return;
	if (currentData?.presentation) {
		event.preventDefault();
		if (event.deltaY>0) deck?.next(); else deck?.prev();
		return;
	}
	if (!currentData?.previewNavigation) return;
	event.preventDefault();
	send({type:'mdz-slide-navigate', offset:event.deltaY>0?1:-1});
}, {passive:false});
document.addEventListener('contextmenu', event => {
	if (!currentData?.presentation) return;
	event.preventDefault();
	send({type:'mdz-slide-exit'});
});
document.addEventListener('click', event => {
	// 一覧ではreveal.jsのページ選択を優先します。
	if (document.querySelector('.reveal.overview')) return;
	const link = (event.target as Element).closest('a');
	if (link) {
		event.preventDefault();
		const section = link.closest<HTMLElement>('section');
		send({type:'mdz-slide-link', href:link.getAttribute('href') || '', file:currentData?.slides.find(s=>s.id===section?.dataset.id)?.file});
		return;
	}
	if (!currentData?.presentation || event.button!==0 || event.ctrlKey || event.metaKey || event.altKey) return;
	const bounds = document.querySelector('.slides>section.present')?.getBoundingClientRect();
	if (!bounds) return;
	if (event.clientX<=bounds.left+bounds.width*.3) deck?.prev();
	else if (event.clientX>=bounds.right-bounds.width*.3) deck?.next();
}, true);
send({type:'mdz-slide-ready'});
