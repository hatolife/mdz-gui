import { History, TextState } from './history.js';
import { NativeView } from './nvim.js';
interface Snapshot { filename: string; entry: string; pages: string[]; assets: string[]; mode: string; singleMarkdown: boolean; dirty: boolean; id: string; workDir: string; engine: string; nativeError: string }
interface TocEntry { id: number; kind: string; title: string; target: string; name: string; depth: number; missing: boolean }
interface BookContents { revision: string; entries: TocEntry[]; unlisted: string[]; canUndo: boolean; canRedo: boolean }
interface BookInfo { present: boolean; title: string; detected: boolean; source: string; executable: string; winget: boolean; url: string; error: string; trusted: boolean }
interface Settings { mdbookPath: string; mdbookDeclined: boolean; theme: string; accent: string; editor: string; nvimPath: string; initMode: string; initPath: string; undoLevels: number; fontFamily: string; fontSize: number; imageDirectory: string; imageName: string; autoSave: boolean; autoSaveSeconds: number; backupGenerations: number; backupMiB: number }
interface Recovery { id: string; filename: string; updated: string }
interface Slide { id: string; file: string; title: string; layout: string; fontSize: number; background: string; notes: string }
interface SlideDeck { version: number; title: string; theme: string; aspect: string; marginColor?: string; contentMarginX?: number; contentMarginY?: number; fontFamily?: string; bodyFontSize?: number; h1FontSize?: number; h2FontSize?: number; h3FontSize?: number; h4FontSize?: number; h5FontSize?: number; slides: Slide[] }
interface SlidesInfo { deck: SlideDeck; revision: string; canUndo: boolean; canRedo: boolean }
interface Dependency {name:string;found:boolean;path:string;message:string}
interface MarkdownHeading { id: string; text: string; level: number }
interface PresentationState {id:string;slides?:Array<Slide & {html:string}>;index:number;fullscreen:boolean;ready:boolean;closed:boolean}
interface Backend {
	CheckDependencies(nvimPath:string,initPath:string,mdbookPath:string):Promise<Dependency[]>;
	StartPresentation(slides:Array<Slide & {html:string}>,index:number):Promise<PresentationState>; PresentationState():Promise<PresentationState>; PresentationCommand(command:string,index:number):Promise<void>;StopPresentation():Promise<void>;
	Slides(): Promise<SlidesInfo>; PrepareSlides(): Promise<SlidesInfo>; ChangeSlide(revision: string, id: string, operation: string, value: string): Promise<SlidesInfo>; ConfigureSlides(revision: string, deck: SlideDeck): Promise<SlidesInfo>; RenderSlide(text: string, layout: string): Promise<string>;
	ConvertDocumentMode(target: string): Promise<void>;
	Contents(): Promise<BookContents>; ChangeContents(revision: string, index: number, operation: string, value: string): Promise<BookContents>;
	RenameBook(revision: string, title: string): Promise<void>; GetBookConfiguration(): Promise<{text: string; revision: string}>; SaveBookConfiguration(revision: string, text: string): Promise<void>;
	ResolveUnsaved(choice: string): Promise<void>; NewDocument(kind: string): Promise<boolean>; EndEditing(): Promise<void>; BookStatus(): Promise<BookInfo>; StartBook(allow: boolean): Promise<string>; StopBook(): Promise<void>; InstallMdbook(): Promise<string>; ChooseMdbook(): Promise<string>;
	Initial(): Promise<string>; Version(): Promise<string>; State(): Promise<Snapshot>; MarkDirty(): Promise<void>;
	New(): Promise<boolean>; Open(name: string): Promise<boolean>; OpenInNewWindow(name: string): Promise<void>; ImportMarkdownFiles(names: string[], target: string, after: boolean): Promise<string[]>; Text(name: string): Promise<string>;
	Update(name: string, text: string): Promise<void>; AddPage(name: string): Promise<void>; MovePage(expected: string[], name: string, target: string, after: boolean): Promise<void>;
	ImportImage(filename: string): Promise<string>; Save(as: boolean): Promise<boolean>; AddImage(): Promise<string>; StoreImage(base64: string): Promise<string>; Render(text: string): Promise<string>;
	Settings(): Promise<Settings>; Configure(settings: Settings): Promise<void>; AutoSave(): Promise<boolean>;
	ChooseExecutable(): Promise<string>; ChooseInit(): Promise<string>; OpenDataFolder(): Promise<void>;
	Recoveries(): Promise<Recovery[]>; Recover(id: string): Promise<boolean>;
	StartNative(): Promise<boolean>; NativeOpen(name: string): Promise<void>;
	NativePoll(): Promise<{name: string; text: string; changed: boolean; canUndo: boolean; canRedo: boolean}>;
	NativeInput(keys: string): Promise<void>; NativePaste(text: string): Promise<void>; NativeUndo(redo: boolean): Promise<void>;
	NativeResize(cols: number, rows: number): Promise<void>; NativeScroll(ratio: number): Promise<void>; NativeMouse(button: string, action: string, modifier: string, row: number, col: number): Promise<void>;
}
declare global { interface Window { go: { main: { App: Backend } }; runtime: { OnFileDrop(callback: (x: number, y: number, paths: string[]) => void, useDropTarget: boolean): void; ResolveFilePaths?(x: number, y: number, files: File[]): void; EventsOn(event: string, callback: (...args: any[]) => void): void; BrowserOpenURL(url: string): void; WindowMinimise?(): void; WindowToggleMaximise?(): void; Quit?(): void; WindowFullscreen?(): void; WindowUnfullscreen?(): void; WindowIsFullscreen?(): Promise<boolean> } } }
const api = window.go.main.App;
const element = <T extends HTMLElement>(id: string): T => document.getElementById(id) as T;
void api.Version().then(version => { element('app-version').textContent = version; }).catch(() => {});
const editor = element<HTMLTextAreaElement>('editor');
const nativeInput = element<HTMLTextAreaElement>('native-input');
const preview = element<HTMLIFrameElement>('preview');
const editorEngine = element<HTMLElement>('editor-engine');
let current = '', changed = false, busy = false, renderID = 0, timer = 0, checkpointTimer = 0;
let state: Snapshot;
let cfg: Settings;
let markPending: Promise<void> = Promise.resolve();
let nativePending: Promise<void> = Promise.resolve();
let nativeSaveRequested = false;
let activePoll: Promise<void> = Promise.resolve();
let composing = false;
let compositionBefore: TextState | undefined;
let activeEngine = '';
let nativeCanUndo = false;
let nativeCanRedo = false;
let scrollSyncLocked = false;
let scrollSyncRatio = 0;
let nativeScrollTimer = 0;
const scrollPositions = new Map<string, number>();
let autoDeadline = 0;
let currentSession = '';
let editing = false;
let bookInfo: BookInfo | undefined;
let bookFallback = false;
let contents: BookContents | undefined;
let tocEditing = false;
let tocSelected = -1;
let tocReturnPage = '';
let configRevision = '';
let tocOperation = '';
let selectedTocName = '';
const collapsed = new Set<string>();
const histories = new Map<string, History>();
let beforeInput: TextState | undefined;
let slidesInfo: SlidesInfo | undefined;
let slideSettings: SlidesInfo | undefined;
let presentationIndex = 0;
let presentationToken = 0;
let thumbnailEpoch = 0;
let slideDebugOutline = false;
let slideTypographyTimer = 0;
let slideTypographyGeneration = 0;
let slideTypographyDesired: SlideDeck | undefined;
let slideTypographyPending: Promise<void> = Promise.resolve();
const slideFrame = element<HTMLIFrameElement>('slide-preview');
const presentationFrame = element<HTMLIFrameElement>('presentation-frame');
const framePayloads = new Map<HTMLIFrameElement, Record<string, unknown>>();

function status(message: string, error = false): void { element('status').textContent = message; element('status').title = message; element('status').classList.toggle('error', error); }
function history(): History { let h = histories.get(current); if (!h) { h = new History(cfg.undoLevels); histories.set(current, h); } return h; }
function setEditorEngine(engine: string): void {
	for (const button of editorEngine.querySelectorAll<HTMLButtonElement>('[data-engine]')) {
		const selected = button.dataset.engine === engine;
		button.classList.toggle('selected', selected);
		button.setAttribute('aria-pressed', String(selected));
	}
}
function updateUndoRedo(): void {
	const undo = element<HTMLButtonElement>('undo');
	const redo = element<HTMLButtonElement>('redo');
	let canUndo = false, canRedo = false;
	if (editing && state?.id) {
		if (state.engine === 'neovim') {
			canUndo = nativeCanUndo;
			canRedo = nativeCanRedo;
		} else {
			const h = histories.get(current);
			canUndo = !!h?.canUndo();
			canRedo = !!h?.canRedo();
		}
	}
	undo.disabled = busy || !canUndo;
	redo.disabled = busy || !canRedo;
	undo.title = canUndo ? '元に戻す' : '元に戻せる変更はありません';
	redo.title = canRedo ? 'やり直す' : 'やり直せる変更はありません';
}
function textState(): TextState { return {text: editor.value, start: editor.selectionStart, end: editor.selectionEnd}; }
function enqueueNative(work: () => Promise<void>): void { nativePending = nativePending.then(work).catch(error => status(String(error), true)); }
const native = new NativeView(element('native'), element<HTMLCanvasElement>('native-canvas'), nativeInput, {
	input: keys => { if (!busy && editing) enqueueNative(() => api.NativeInput(keys)); },
	paste: text => { if (!busy && editing) enqueueNative(() => api.NativePaste(text)); },
	resize: (cols, rows) => { void api.NativeResize(cols, rows).catch(error => status(String(error), true)); },
	mouse: (button, action, modifier, row, col) => { if (!busy && editing) void api.NativeMouse(button, action, modifier, row, col).catch(error => status(String(error), true)); },
});

let flushPending: Promise<void> = Promise.resolve();
async function action(work: () => Promise<void>): Promise<void> {
	if (busy) return;
	const focus = document.activeElement;
	busy = true; editor.disabled = true; nativeInput.disabled = true;
	document.querySelectorAll('button').forEach(button => { if (!button.closest('#window-controls')) button.disabled = true; });
	try { await activePoll; await markPending; await nativePending; await flushPending; await flushSlideTypography(true); await slideTypographyPending; await work(); }
	catch (error) { status(String(error), true); }
	finally {
		busy = false; editor.disabled = !editing; nativeInput.disabled = !editing;
		document.querySelectorAll('button').forEach(button => button.disabled = false);
		updateUndoRedo();
		if (focus === editor && state?.engine === 'builtin') editor.focus({preventScroll:true});
		if (focus === nativeInput && state?.engine === 'neovim') native.focus();
	}
}
function flush(): Promise<void> {
	const pending = flushPending.catch(() => {}).then(async () => {
		await markPending;
		if (!state?.id || !changed || state.engine !== 'builtin') return;
		const id = state.id, file = current, text = editor.value;
		await api.Update(file, text);
		// Editing remains available during recovery checkpoints. Do not clear newer edits.
		if (state?.id === id && current === file && editor.value === text) changed = false;
		if (file === bookSummaryName()) {
			contents = await api.Contents();
			await refreshSidebar();
		}
	});
	flushPending = pending.catch(() => {});
	return pending;
}
function bookSummaryName(): string {
	if (!bookInfo?.present) return '';
	return bookInfo.source === '.' ? 'SUMMARY.md' : `${bookInfo.source || 'src'}/SUMMARY.md`;
}
function refreshTitle(): void {
	const unsaved = !!state?.id && (changed || !!state?.dirty);
	element('filename').textContent = `${state?.id ? state.filename || '新しい文書' : ''}${unsaved ? ' ●' : ''}`;
	const save = element<HTMLButtonElement>('save');
	save.classList.toggle('unsaved', unsaved);
	const saveLabel = state?.singleMarkdown ? 'Markdownを上書き保存' : '保存';
	save.title = unsaved ? `${saveLabel}（未保存の変更あり）` : saveLabel;
	save.setAttribute('aria-label', save.title);
	const saveAs = element<HTMLButtonElement>('save-as');
	saveAs.title = state?.singleMarkdown ? 'MDZとして保存' : '名前を付けて保存';
	saveAs.setAttribute('aria-label', saveAs.title);
	element('save-hint').textContent = state?.singleMarkdown ? 'Ctrl+S Markdown保存 · Ctrl+Shift+S MDZとして保存 · Ctrl+O 開く' : 'Ctrl+S 保存 · Ctrl+O 開く';
	element('current').textContent = state?.mode === 'slides' ? slidesInfo?.deck.slides.find(s=>s.file===current)?.title || current : bookInfo?.present ? contents?.entries.find(e=>e.name===current)?.title || current.split('/').pop() || '' : current;
	element('current').title=current;
	element('filename').title = state?.filename || '';
	element('count').textContent = `${editor.value.length.toLocaleString()} 文字`;
	const engine = state?.engine === 'neovim' ? 'neovim' : 'builtin';
	element('engine').textContent = engine === 'neovim' ? 'Neovim' : '内蔵エディター';
	setEditorEngine(engine);
	updateUndoRedo();
}
let documentView: 'pages' | 'files' = 'pages';
let draggedPage = '';
let markdownHeadingPage = '';
let markdownHeadings: MarkdownHeading[] = [];
let activeMarkdownHeading = '';
function updateMarkdownHeadingHighlight(id: string): void {
	const changed = activeMarkdownHeading !== id;
	activeMarkdownHeading = id;
	let activeButton: HTMLButtonElement | undefined;
	for (const button of element('pages').querySelectorAll<HTMLButtonElement>('.markdown-heading')) {
		const active = button.dataset.headingId === id;
		button.classList.toggle('active-heading', active);
		if (active) {
			button.setAttribute('aria-current','location');
			activeButton = button;
		} else button.removeAttribute('aria-current');
	}
	if (changed && activeButton) activeButton.scrollIntoView({block:'nearest'});
}
function syncMarkdownHeadingHighlight(): void {
	if (markdownHeadingPage !== current || !markdownHeadings.length) {
		updateMarkdownHeadingHighlight('');
		return;
	}
	const doc = preview.contentDocument;
	if (!doc) return;
	const threshold = 96;
	let active = markdownHeadings[0].id;
	for (const heading of markdownHeadings) {
		const target = doc.getElementById(heading.id);
		if (!target) continue;
		if (target.getBoundingClientRect().top <= threshold) active = heading.id;
		else break;
	}
	updateMarkdownHeadingHighlight(active);
}
function scrollToMarkdownHeading(id: string): void {
	const target = preview.contentDocument?.getElementById(id);
	if (!target) return;
	updateMarkdownHeadingHighlight(id);
	target.scrollIntoView({behavior:'smooth',block:'start'});
}
function appendMarkdownHeadings(nav: HTMLElement): void {
	if (markdownHeadingPage !== current) return;
	for (const heading of markdownHeadings) {
		const b = document.createElement('button');
		b.className = 'markdown-heading';
		b.dataset.level = String(heading.level);
		b.dataset.page = current;
		b.dataset.headingId = heading.id;
		b.classList.toggle('active-heading', heading.id === activeMarkdownHeading);
		if (heading.id === activeMarkdownHeading) b.setAttribute('aria-current','location');
		b.textContent = heading.text;
		b.title = `${'#'.repeat(heading.level)} ${heading.text}`;
		b.style.paddingLeft = `${28 + (heading.level - 1) * 14}px`;
		b.onclick = () => scrollToMarkdownHeading(heading.id);
		nav.append(b);
	}
}
function renderMarkdownPages(nav: HTMLElement): void {
	nav.replaceChildren();
	for (const name of state.pages) {
		addFileButton(nav,name,name.split('/').pop()!,0);
		if (name === current) appendMarkdownHeadings(nav);
	}
}
function renderSingleMarkdown(nav: HTMLElement): void {
	nav.replaceChildren();
	appendMarkdownHeadings(nav);
}
async function refreshSidebar(): Promise<void> {
	state = await api.State();
	const nav = element('pages'); const scrollTop = element('sidebar').scrollTop;
	if(state.mode!=='slides') { nav.replaceChildren(); slideListKey=''; }
	if (!state.id) { refreshTitle(); return; }
	element('document-root').textContent = '▣ ' + (state.filename.split(/[\\/]/).pop() || '新しい文書.mdz');
	const isSlides = state.mode === 'slides';
	const isSingle = state.singleMarkdown;
	const isBook = !isSlides && !isSingle && !!bookInfo?.present;
	element('document-to-slides').hidden = !editing || isSlides || isBook || isSingle;
	element('slides-to-document').hidden = !editing || !isSlides;
	document.body.classList.toggle('slides-mode', isSlides);
	element('slides-heading').hidden = !isSlides; element('slides-toolbar').hidden = !isSlides;
	element('slide-tools').hidden = true;
	if (!isSlides) { slideFrame.hidden = true; element('slide-overflow').hidden = true; slidesInfo = undefined; }
	document.body.classList.toggle('book-mode', isBook);
	element('tree-tabs').hidden = isBook || isSlides || isSingle;
	element('document-root').hidden = isBook || isSlides || isSingle;
	element('book-heading').hidden = !isBook;
	element('book-sidebar-bottom').hidden = !isBook || !editing;
	element('book-title').textContent = bookInfo?.title || 'mdBook';
	element('book-title-edit').hidden = !editing;
	element('add-page').hidden = !editing || isBook || isSlides || isSingle;
	element('image').hidden = !editing || isSingle;
	element('recovery').hidden = isSingle;
	element('toc-edit').hidden = !editing;
	element('toc-tools').hidden = !editing || !tocEditing;
	element('toc-hint').hidden = !editing || !tocEditing;
	element('toc-item-tools').hidden = !editing || !tocEditing || tocSelected < 0;
	element('book-unlisted').hidden = true;
	if (isSlides) {
		slidesInfo = await api.Slides(); renderSlideList(nav);
	} else if (isBook) {
		try { contents = await api.Contents(); renderContents(nav); }
		catch(error) { nav.textContent = String(error); element('toc-tools').hidden = true; element('toc-item-tools').hidden = true; }
	} else if (isSingle) {
		renderSingleMarkdown(nav);
	} else if(documentView==='pages') {
		renderMarkdownPages(nav);
	} else {
		const folders = new Set<string>();
		const files = [...state.pages, ...state.assets];
		for (const name of files) { const parts = name.split('/'); for (let i=1;i<parts.length;i++) folders.add(parts.slice(0,i).join('/')); }
		const walk = (parent: string, depth: number): void => {
			const children = [...folders].filter(p => p.split('/').slice(0,-1).join('/') === parent).sort();
			for (const folder of children) {
				const b = document.createElement('button'); b.textContent = (collapsed.has(folder) ? '▸ ' : '▾ ') + folder.split('/').pop(); b.style.paddingLeft = `${10+depth*16}px`; b.setAttribute('aria-expanded', String(!collapsed.has(folder))); b.onclick = () => { if(collapsed.has(folder)) collapsed.delete(folder); else collapsed.add(folder); void refreshSidebar(); }; nav.append(b);
				if (!collapsed.has(folder)) walk(folder,depth+1);
			}
			for (const name of files.filter(p => p.split('/').slice(0,-1).join('/') === parent).sort()) addFileButton(nav,name,name.split('/').pop()!,depth);
		}; walk('',0);
	}
	element('sidebar').scrollTop=scrollTop;
	refreshTitle();
}
for(const view of ['pages','files'] as const) element(`${view}-tab`).onclick=()=>{
	documentView=view;
	for(const other of ['pages','files']) element(`${other}-tab`).classList.toggle('selected',view===other);
	void refreshSidebar();
};
function addFileButton(nav: HTMLElement, name: string, label: string, depth: number): void {
	const b = document.createElement('button'); b.textContent = `${name === state.entry ? '⌂ ' : ''}${label}`; b.title = name; b.style.paddingLeft = `${10+depth*16}px`;
	const currentPage = state.pages.includes(name) && name === current;
	b.classList.toggle('selected', currentPage);
	b.classList.toggle('current-page', currentPage);
	if (currentPage) b.setAttribute('aria-current','page');
	b.onclick = () => void action(async () => {
		await flush();
		if (state.pages.includes(name)) { await selectPage(name); }
		else { bookFallback = true; element('book-preview').hidden = true; preview.hidden = false; preview.removeAttribute('src'); preview.onload = null;
			if (/\.(png|jpe?g|gif|webp|svg)$/i.test(name)) preview.srcdoc = `<body style="margin:0;display:grid;place-items:center"><img style="max-width:100%;max-height:100vh" src="/bundle/${name.split('/').map(encodeURIComponent).join('/')}"></body>`;
			else { preview.srcdoc = ''; status(`${name}（同梱ファイル）`); }
		}
	});
	b.dataset.page = name;
	if(documentView==='pages' && editing && state.pages.includes(name)) {
		b.draggable=true;
		b.ondragstart=event=>{draggedPage=name;event.dataTransfer?.setData('application/x-mdz-page',name);if(event.dataTransfer)event.dataTransfer.effectAllowed='move';};
		const clear=()=>{nav.querySelectorAll('.drop-before,.drop-after').forEach(node=>node.classList.remove('drop-before','drop-after'));};
		b.ondragend=()=>{draggedPage='';clear();};
		b.ondragover=event=>{
			if(!draggedPage || busy || !event.dataTransfer?.types.includes('application/x-mdz-page'))return;
			event.preventDefault();clear();
			const bounds=b.getBoundingClientRect();b.classList.add(event.clientY>bounds.top+bounds.height/2?'drop-after':'drop-before');
		};
		b.ondragleave=()=>b.classList.remove('drop-before','drop-after');
		b.ondrop=event=>{
			const source=draggedPage;draggedPage='';clear();
			if(!source || busy)return;
			event.preventDefault();event.stopPropagation();
			const bounds=b.getBoundingClientRect(), after=event.clientY>bounds.top+bounds.height/2, expected=[...state.pages];
			void action(async()=>{await flush();try{await api.MovePage(expected,source,name,after);status('ページの順序を変更しました');}finally{await refreshSidebar();}});
		};
	}
	nav.append(b);
}
async function selectPage(name: string): Promise<void> {
	++renderID;
	if (current && current !== name) scrollPositions.set(current, scrollSyncRatio);
	scrollSyncRatio = scrollPositions.get(name) ?? 0;
	if (editing && state.engine === 'neovim') {
		nativeCanUndo = false; nativeCanRedo = false; updateUndoRedo();
		await api.NativeOpen(name);
		await api.NativeScroll(scrollSyncRatio);
	}
	const text = await api.Text(name);
	current = name; editor.value = text; changed = false; beforeInput = undefined;
	markdownHeadingPage = '';
	markdownHeadings = [];
	activeMarkdownHeading = '';
	await refreshSidebar(); ensureCurrentTocVisible(); await render();
	if(bookInfo?.url && !bookFallback && state.pages.includes(current)) showBook(bookInfo.url);
	requestAnimationFrame(() => syncScroll('state', scrollSyncRatio, true));
}
function applyEditor(): void {
	if (activeEngine && activeEngine !== state.engine) {
		histories.clear();
		nativeCanUndo = false;
		nativeCanRedo = false;
	}
	activeEngine = state.engine;
	const isNative = state.engine === 'neovim';
	editor.hidden = isNative; native.setActive(isNative && editing); native.configure(cfg.fontFamily, cfg.fontSize);
	editor.style.fontFamily = cfg.fontFamily; editor.style.fontSize = `${cfg.fontSize}px`;
	element('native-warning').hidden = !state.nativeError;
	element('native-warning').textContent = state.nativeError;
	setEditorEngine(isNative ? 'neovim' : 'builtin');
	updateUndoRedo();
}
async function reload(startEditing = false): Promise<void> {
	element<HTMLSelectElement>('preview-kind').value='auto'; element('preview-kind').hidden=true;
	state = await api.State(); editing = false; bookFallback = false; bookInfo=undefined; contents=undefined; tocEditing=false; tocSelected=-1;
	if (state.singleMarkdown) documentView = 'pages';
	element<HTMLIFrameElement>('book-preview').src='about:blank'; element('book-preview').hidden=true; preview.hidden=false;
	closePresentation(); slideFrame.hidden=true; slidesInfo=undefined;
	if (currentSession !== state.id) {
		histories.clear(); collapsed.clear(); native.reset(); scrollPositions.clear();
		scrollSyncRatio = 0; current = ''; currentSession = state.id;
	}
	element('welcome').hidden = !!state.id; element('workspace').hidden = !state.id; element('editing').hidden = !state.id;
	for (const id of ['save','save-as','sidebar-toggle']) element(id).hidden = !state.id;
	updateEditing();
	if (!state.id) { current = ''; refreshTitle(); return; }
	if (startEditing) { editing = true; if (cfg.editor === 'neovim') await api.StartNative(); state = await api.State(); updateEditing(); }
	bookInfo = await api.BookStatus();
	let first = state.entry;
	if(bookInfo.detected) { contents=await api.Contents(); first=contents.entries.find(e=>e.name && !e.missing)?.name || state.entry; }
	applyEditor(); await selectPage(first); await refreshBook(); status(editing ? '編集モード' : '表示モード');
}
function updateSlideDebugToggle(): void {
	const toggle=element<HTMLButtonElement>('slide-debug-outline');
	toggle.hidden=!editing || state?.mode!=='slides';
	toggle.setAttribute('aria-checked',String(slideDebugOutline));
	toggle.title=slideDebugOutline ? '要素の枠・余白表示を消す' : '要素の枠・余白を表示する';
}
function updateEditing(): void {
	document.body.classList.toggle('editing',editing); element('editing').setAttribute('aria-checked', String(editing));
	element('editing').title = editing ? '表示モードに切り替える' : '編集モードに切り替える';
	element('panes').className = editing ? 'split' : 'view'; editor.readOnly = !editing; nativeInput.disabled = !editing;
	editorEngine.hidden = !editing; element('engine').hidden = true;
	element('slide-typography').hidden = !editing || state?.mode !== 'slides';
	updateSlideDebugToggle();
	element('slide-overflow-actions').hidden = !editing;
	for (const id of ['undo','redo','edit-mode','split-mode','view-mode','add-page','image']) {
		element(id).hidden = !editing || (state?.singleMarkdown && (id === 'add-page' || id === 'image'));
	}
	for (const mode of ['edit','split','view']) {
		const button = element<HTMLButtonElement>(`${mode}-mode`);
		const selected = mode === (editing ? 'split' : 'view');
		button.classList.toggle('selected', selected);
		button.setAttribute('aria-pressed', String(selected));
	}
	native.setActive(editing && state?.engine === 'neovim');
	updateUndoRedo();
}
element('editing').onclick = () => void action(async () => {
	await flush();
	if (editing) { await api.EndEditing(); editing = false; tocEditing=false; }
	else { editing = true; if (cfg.editor === 'neovim') await api.StartNative(); }
	state = await api.State();
	updateEditing(); await refreshSidebar(); applyEditor(); await selectPage(current); native.resize();
});
for (const button of editorEngine.querySelectorAll<HTMLButtonElement>('[data-engine]')) button.onclick = () => {
	const requested = button.dataset.engine!;
	void action(async () => {
		if (!editing || requested === state.engine) return;
		if (requested === 'neovim') {
			await flush();
			await api.StartNative();
			state = await api.State();
			applyEditor();
			if (state.engine !== 'neovim') {
				setEditorEngine('builtin');
				status(state.nativeError || 'Neovimを起動できません', true);
				return;
			}
			nativeCanUndo = false; nativeCanRedo = false;
			await api.NativeOpen(current);
			await api.NativeScroll(scrollSyncRatio);
			native.resize();
			window.setTimeout(() => syncScroll('state', scrollSyncRatio, true), 50);
			native.focus();
			updateUndoRedo();
			status('Neovimに切り替えました');
			return;
		}
		await api.EndEditing();
		state = await api.State();
		editor.value = await api.Text(current);
		changed = false;
		applyEditor();
		await render();
		requestAnimationFrame(() => syncScroll('state', scrollSyncRatio, true));
		editor.focus({preventScroll:true});
		status('内蔵エディターに切り替えました');
	});
};
function applyTheme(): void {
	document.documentElement.classList.toggle('dark', cfg.theme === 'dark' || (cfg.theme === 'system' && matchMedia('(prefers-color-scheme: dark)').matches));
	document.documentElement.dataset.accent = cfg.accent;
}
matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => { if(cfg) { applyTheme(); if(state?.id) void render(); } });
// mdBook未導入時にインストールするかを確認し、インストールする場合はtrueを返す。
function askMdbookInstall(): Promise<boolean> {
	const dialog = element<HTMLDialogElement>('mdbook-install-dialog');
	const accept = element<HTMLButtonElement>('mdbook-install-accept');
	const decline = element<HTMLButtonElement>('mdbook-install-decline');
	return new Promise(resolve => {
		const finish = (result: 'accept' | 'decline' | 'cancel') => {
			accept.onclick = null; decline.onclick = null; dialog.oncancel = null;
			dialog.close();
			if (result === 'decline') { cfg.mdbookDeclined = true; void api.Configure(cfg).catch(error => status(String(error), true)); } // 拒否は設定へ保持する。
			resolve(result === 'accept');
		};
		accept.disabled = false; decline.disabled = false; // action実行中は全ボタンが無効化されるため戻す。
		accept.onclick = () => finish('accept');
		decline.onclick = () => finish('decline');
		dialog.oncancel = event => { event.preventDefault(); finish('cancel'); }; // Escは今回だけ使わない扱いにする。
		dialog.showModal();
	});
}
async function refreshBook(askInstall = false): Promise<void> {
	bookInfo = await api.BookStatus();
	const panel = element('book-panel');
	panel.hidden = true;
	if (bookInfo.detected && !bookInfo.url && !bookFallback && !bookInfo.error && !bookInfo.executable && bookInfo.winget) {
		if ((cfg.mdbookDeclined && !askInstall) || !(await askMdbookInstall())) {
			bookFallback = true; // mdBookを使わず通常のMDZとして表示する。
			element<HTMLSelectElement>('preview-kind').value = 'markdown';
			status('mdBookを使わずに表示しています。設定からインストールできます。');
		}
	}
	if (bookInfo.detected && !bookInfo.url && !bookFallback && !bookInfo.error) {
		try {
			if (!bookInfo.executable) {
				status('wingetでmdBookをインストールしています…');
				cfg.mdbookPath = await api.InstallMdbook();
				await api.Configure(cfg);
			}
			status('mdBookを表示しています…');
			await flush();
			await api.StartBook(true);
			bookInfo = await api.BookStatus();
		} catch (error) {
			bookInfo = await api.BookStatus();
			bookInfo.error = String(error);
		}
	}
	element('preview-kind').hidden = !bookInfo.detected;
	panel.hidden = bookFallback || !bookInfo.error;
	element('book-message').textContent = bookInfo.error;
	element('book-install').hidden = !!bookInfo.executable || !bookInfo.winget;
	element('book-start').hidden = !bookInfo.executable || !bookInfo.detected;
	if (bookInfo.url && !bookFallback) showBook(bookInfo.url);
	await refreshSidebar();
}
function bookPagePath(name: string): string {
	const prefix=bookInfo?.source === '.' ? '' : (bookInfo?.source || 'src')+'/';
	return name.startsWith(prefix) ? name.slice(prefix.length).replace(/(^|\/)README\.md$/,'$1index.md').replace(/\.(md|markdown)$/i,'.html').split('/').map(encodeURIComponent).join('/') : '';
}
function showBook(url: string): void {
	if (current === bookSummaryName()) { void render(); return; }
	const frame=element<HTMLIFrameElement>('book-preview'); const target=url+bookPagePath(current);
	frame.onload=()=>requestAnimationFrame(()=>syncScroll('state',scrollSyncRatio,true));
	if(frame.src!==target) frame.src=target;
	else requestAnimationFrame(()=>syncScroll('state',scrollSyncRatio,true));
	frame.hidden=false;preview.hidden=true;element('book-panel').hidden=true;
}
window.addEventListener('message',event=>{
	const frame=element<HTMLIFrameElement>('book-preview');
	if(!bookInfo?.url || event.source!==frame.contentWindow || event.origin!==new URL(bookInfo.url).origin) return;
	if(event.data?.type==='mdz-book-save') { void save(event.data.saveAs===true); return; }
	if(event.data?.type==='mdz-book-scroll') {
		const ratio=Number(event.data.ratio);
		if(Number.isFinite(ratio)) syncScroll('book',ratio);
		return;
	}
	if(event.data?.type!=='mdz-book-page') return;
	const p=String(event.data.path).replace(/^\//,'');
	const name=contents?.entries.find(e=>e.name&&!e.missing&&bookPagePath(e.name)===p)?.name || (p===''||p==='index.html' ? contents?.entries.find(e=>e.name&&!e.missing)?.name : undefined);
	if(name && name!==current && !busy) void action(async()=>{await flush();await selectPage(name);});
	else if(name===current) requestAnimationFrame(()=>syncScroll('state',scrollSyncRatio,true));
});
element('book-start').onclick = () => void action(async () => { await flush(); const url = await api.StartBook(true); bookFallback = false; await refreshBook(); showBook(url); });
element('book-install').onclick = () => void action(async () => { status('wingetでmdBookをインストールしています…'); const installed = await api.InstallMdbook(); cfg.mdbookPath=installed; cfg.mdbookDeclined=false; await api.Configure(cfg); await refreshBook(); status('mdBookを導入しました。'); });
element('book-path').onclick = () => void action(async () => { const p = await api.ChooseMdbook(); if (p) { cfg.mdbookPath=p; await api.Configure(cfg); await refreshBook(); } });
element('book-help').onclick = () => window.runtime.BrowserOpenURL('https://rust-lang.github.io/mdBook/guide/installation.html');
element('book-fallback').onclick = () => void action(async () => { bookFallback=true; element<HTMLSelectElement>('preview-kind').value='markdown'; await api.StopBook(); if(bookInfo) bookInfo.url=''; element('book-preview').hidden=true; preview.hidden=false; element('book-panel').hidden=true; await render(); });
// タイトルバーを使わず、既存の終了確認を経由してウィンドウを操作します。
element('window-minimise').onclick = () => window.runtime.WindowMinimise?.();
element('window-maximise').onclick = () => window.runtime.WindowToggleMaximise?.();
element('window-close').onclick = () => window.runtime.Quit?.();
element('sidebar-toggle').onclick = () => { const hide = !element('sidebar').hidden; element('sidebar').hidden=hide; element('sidebar-resizer').hidden=hide; element('sidebar-toggle').setAttribute('aria-expanded',String(!hide)); native.resize(); };
element('preview-kind').onchange = () => void action(async () => {
	bookFallback = element<HTMLSelectElement>('preview-kind').value === 'markdown';
	if (bookFallback) { await api.StopBook(); if(bookInfo) bookInfo.url=''; element('book-preview').hidden=true; preview.hidden=false; element('book-panel').hidden=true; await render(); }
	else await refreshBook(true);
});
const resizer = element('sidebar-resizer');
function sidebarWidth(width: number): void { const n=Math.max(170,Math.min(width,Math.min(480,innerWidth*.45))); element('sidebar').style.width=`${n}px`; localStorage.setItem('sidebar-width',String(n)); native.resize(); }
sidebarWidth(Number(localStorage.getItem('sidebar-width'))||240);
resizer.onpointerdown = event => { resizer.setPointerCapture(event.pointerId); };
resizer.onpointermove = event => { if(resizer.hasPointerCapture(event.pointerId)) sidebarWidth(event.clientX - element('workspace').getBoundingClientRect().left); };
resizer.onkeydown = event => { if(['ArrowLeft','ArrowRight'].includes(event.key)) { event.preventDefault(); sidebarWidth(element('sidebar').offsetWidth+(event.key==='ArrowRight'?16:-16)); } };

// 相対リンクが文書ルートより上に出ないことを検査します。
function resolveLink(reference: string, source = current): { path: string; hash: string } | null {
	if (/^[a-z][a-z\d+.-]*:/i.test(reference) || reference.startsWith('/') || reference.includes('\\')) return null;
	const hashIndex = reference.indexOf('#');
	const hash = hashIndex >= 0 ? reference.slice(hashIndex) : '';
	let name = reference.split('#')[0].split('?')[0];
	try { name = decodeURIComponent(name); } catch { return null; }
	if (!name) return { path: source, hash };
	if (name.startsWith('/') || name.includes('\\')) return null;
	const parts = source.split('/').slice(0, -1);
	for (const part of name.split('/')) {
		if (part === '..') { if (!parts.length) return null; parts.pop(); }
		else if (part && part !== '.') parts.push(part);
	}
	return { path: parts.join('/'), hash };
}

function resolveSingleMarkdownImage(reference: string): string | null {
	if (/^[a-z][a-z\d+.-]*:/i.test(reference) || reference.startsWith('/') || reference.includes('\\')) return null;
	let name = reference.split('#')[0].split('?')[0];
	try { name = decodeURIComponent(name); } catch { return null; }
	if (!name || name.startsWith('/') || name.includes('\\')) return null;
	return name;
}

type ScrollSource = 'editor' | 'preview' | 'book' | 'native' | 'state';

function scrollRatio(top: number, height: number, viewport: number): number {
	const range = Math.max(0, height - viewport);
	return range > 0 ? Math.max(0, Math.min(1, top / range)) : 0;
}
function clampScrollRatio(ratio: number): number {
	return Number.isFinite(ratio) ? Math.max(0, Math.min(1, ratio)) : 0;
}
function ensureCurrentTocVisible(): void {
	const nav = element('pages');
	const target = nav.querySelector<HTMLElement>('.markdown-heading.active-heading,.toc-page.current-page,.slide-card.selected,button.current-page');
	target?.scrollIntoView({block:'nearest'});
}
function sendNativeScroll(ratio: number): void {
	if (!editing || state?.engine !== 'neovim') return;
	window.clearTimeout(nativeScrollTimer);
	nativeScrollTimer = window.setTimeout(() => {
		void api.NativeScroll(ratio).catch(error => status(String(error), true));
	}, 32);
}
function syncScroll(source: ScrollSource, ratio: number, force = false): void {
	if (!state?.id || (scrollSyncLocked && !force)) return;
	const next = clampScrollRatio(ratio);
	if (!force && Math.abs(next - scrollSyncRatio) < 0.0005) return;
	scrollSyncRatio = next;
	if (current) scrollPositions.set(current, next);
	scrollSyncLocked = true;

	if (source !== 'editor') {
		editor.scrollTop = next * Math.max(0, editor.scrollHeight - editor.clientHeight);
	}
	if (source !== 'preview' && !preview.hidden && state.mode !== 'slides') {
		const win = preview.contentWindow;
		const doc = preview.contentDocument;
		if (win && doc) win.scrollTo(0, next * Math.max(0, doc.documentElement.scrollHeight - win.innerHeight));
	}
	const book = element<HTMLIFrameElement>('book-preview');
	if (source !== 'book' && !book.hidden && bookInfo?.url) {
		book.contentWindow?.postMessage({type:'mdz-book-scroll-to',ratio:next}, new URL(bookInfo.url).origin);
	}
	if (source !== 'native') sendNativeScroll(next);

	requestAnimationFrame(() => {
		if (markdownHeadingPage === current && markdownHeadings.length) syncMarkdownHeadingHighlight();
		scrollSyncLocked = false;
	});
}
editor.addEventListener('scroll', () => syncScroll('editor', scrollRatio(editor.scrollTop, editor.scrollHeight, editor.clientHeight)), {passive:true});

async function render(): Promise<void> {
	if (!state?.id) return;
	if (state.mode === 'slides') { await renderSlidePreview(); return; }
	const summaryPreview = current === bookSummaryName();
	if (!bookInfo?.url || bookFallback || summaryPreview) { preview.hidden=false; element("book-preview").hidden=true; }
	const ticket = ++renderID;
	const sourcePage = current;
	const html = await api.Render(editor.value);
	if (ticket !== renderID || sourcePage !== current) return;
	const doc = new DOMParser().parseFromString(html, 'text/html');
	if (!bookInfo?.present) {
		markdownHeadingPage = sourcePage;
		markdownHeadings = [...doc.querySelectorAll<HTMLElement>('h1,h2,h3,h4,h5,h6')]
			.filter(heading => !!heading.id)
			.map(heading => ({id:heading.id,text:heading.textContent?.trim() || heading.id,level:Number(heading.tagName.slice(1))}));
	}
	for (const img of doc.querySelectorAll('img')) {
		const source = img.getAttribute('src') || '';
		if (state.singleMarkdown) {
			const target = resolveSingleMarkdownImage(source);
			if (target) img.src = `${location.origin}/local-image?path=${encodeURIComponent(target)}`;
			else { img.removeAttribute('src'); img.alt += '（外部画像は非表示）'; }
			continue;
		}
		const target = resolveLink(source);
		if (target) img.src = `${location.origin}/bundle/${target.path.split('/').map(encodeURIComponent).join('/')}`;
		else { img.removeAttribute('src'); img.alt += '（外部画像は非表示）'; }
	}
	const dark = document.documentElement.classList.contains('dark');
	const themeStyle = getComputedStyle(document.documentElement);
	const paperColor = themeStyle.getPropertyValue('--paper').trim() || (dark ? '#242b32' : '#fff');
	const textColor = themeStyle.getPropertyValue('--content-text').trim() || themeStyle.getPropertyValue('--text').trim() || (dark ? '#e1e7eb' : '#2e3d49');
	const mutedColor = themeStyle.getPropertyValue('--muted').trim() || (dark ? '#aab7c1' : '#657581');
	const lineColor = themeStyle.getPropertyValue('--line').trim() || (dark ? '#3c4852' : '#dce3e8');
	const accentColor = themeStyle.getPropertyValue('--accent').trim() || (dark ? '#9eb6c7' : '#607e93');
	if (summaryPreview && contents) {
		const list = document.createElement('nav'); list.className = 'mdbook-summary';
		const levels: number[] = [];
		for (const entry of contents.entries) {
			if (entry.kind === 'raw' || entry.kind === 'heading') continue;
			if (entry.kind === 'separator') { list.append(document.createElement('hr')); continue; }
			if (entry.kind === 'part') { levels.length=0; const h=document.createElement('h3'); h.textContent=entry.title; list.append(h); continue; }
			const a=document.createElement('a'); let number='';
			if(entry.kind==='chapter') { levels.length=entry.depth+1; levels[entry.depth]=(levels[entry.depth]||0)+1; number=levels.map(n=>n||1).join('.')+'. '; }
			a.textContent=number+entry.title+(entry.missing?'（ファイルなし）':''); a.style.paddingLeft=`${10+entry.depth*20}px`;
			if(entry.target) a.setAttribute('href',entry.target); else a.classList.add('draft'); list.append(a);
		}
		doc.body.replaceChildren(list);
	}
	preview.onload = () => {
		const body = preview.contentDocument;
		const win = preview.contentWindow;
		if (!body || !win) return;
		win.addEventListener('scroll', () => syncScroll('preview', scrollRatio(win.scrollY, body.documentElement.scrollHeight, win.innerHeight)), {passive:true});
		requestAnimationFrame(() => syncScroll('state', scrollSyncRatio, true));
		if (!bookInfo?.present && (state.singleMarkdown || documentView === 'pages')) {
			if (state.singleMarkdown) renderSingleMarkdown(element('pages'));
			else renderMarkdownPages(element('pages'));
			preview.contentWindow?.addEventListener('scroll', syncMarkdownHeadingHighlight, {passive:true});
			requestAnimationFrame(syncMarkdownHeadingHighlight);
		}
		body.addEventListener('click', event => {
			const link = (event.target as Element).closest('a');
			if (!link) return;
			event.preventDefault();
			const href = link.getAttribute('href') || '';
			if (/^https?:\/\//i.test(href)) {
				if (confirm('外部ブラウザーで開きますか？\n' + href)) window.runtime.BrowserOpenURL(href);
				return;
			}
			const target = resolveLink(href);
			if (!target) { status('文書外のリンクは開けません', true); return; }
			if (target.path === current && target.hash) {
				try { body.getElementById(decodeURIComponent(target.hash.slice(1)))?.scrollIntoView(); } catch { /* 不正なアンカーは無視します。 */ }
			} else if (state.pages.includes(target.path)) {
				void action(async () => { await flush(); await selectPage(target.path); });
			} else status('このリンクのファイルはプレビューできません');
		});
	};
	// スクリプト、フォーム、外部画像の読み込みをプレビュー内で禁止します。
	preview.srcdoc = `<!doctype html><html lang="ja"><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src ${location.origin}; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"><style>
		:root{color-scheme:${dark ? 'dark' : 'light'}}body{background:${paperColor};color:${textColor};font:15px/1.9 'Segoe UI','Yu Gothic',sans-serif;padding:20px 32px;overflow-wrap:anywhere}.mdbook-summary{max-width:720px;margin:0 auto;padding:18px 8px 48px}.mdbook-summary h3{font-size:1.05rem;margin:26px 0 8px;padding:0 10px 6px;border-bottom:1px solid ${lineColor}}.mdbook-summary a{display:block;padding-top:6px;padding-bottom:6px;padding-right:10px;border-radius:5px;color:${textColor};text-decoration:none}.mdbook-summary a:hover{background:${accentColor}18;color:${accentColor}}.mdbook-summary a.draft{color:${mutedColor}}.mdbook-summary hr{margin:20px 0;border:0;border-top:1px solid ${lineColor}}h1,h2,h3{line-height:1.4}h1{font-size:28px;border-bottom:1px solid ${lineColor};padding-bottom:14px}h2{margin-top:32px;font-size:22px}img{max-width:100%;height:auto}pre{padding:16px;background:${dark ? '#ffffff0d' : '#00000008'};overflow:auto;border-radius:6px}code{font-family:Consolas,monospace;font-size:.9em}table{border-collapse:collapse;width:100%}td,th{border:1px solid ${lineColor};padding:7px 12px;text-align:left}blockquote{border-left:3px solid ${accentColor};padding-left:18px;margin-left:0;color:${mutedColor}}a{color:${accentColor}}hr{border:0;border-top:1px solid ${lineColor}}
	</style></head><body>${doc.body.innerHTML}</body></html>`;
}

function edited(): void {
	if (!editing) return;
	if (!changed) { markPending = api.MarkDirty(); void markPending.catch(error => status(String(error), true)); }
	changed = true; refreshTitle(); status('未保存の変更（作業データを復旧用に保持）');
	window.clearTimeout(timer); timer = window.setTimeout(() => void render().catch(error => status(String(error), true)), 180);
	window.clearTimeout(checkpointTimer); checkpointTimer = window.setTimeout(() => { if (!busy && !composing) void flush().catch(error => status(String(error), true)); }, 350);
}
editor.addEventListener('compositionstart', () => { composing = true; compositionBefore = textState(); });
editor.addEventListener('compositionend', () => { composing = false; if (compositionBefore) history().record(compositionBefore, textState(), 'composition'); compositionBefore = undefined; beforeInput = undefined; edited(); });
editor.addEventListener('beforeinput', event => {
	if (composing) return;
	if (event.inputType === 'historyUndo' || event.inputType === 'historyRedo') { event.preventDefault(); undo(event.inputType === 'historyRedo'); return; }
	beforeInput = textState();
});
editor.addEventListener('input', event => {
	if (composing) { edited(); return; }
	if (beforeInput) history().record(beforeInput, textState(), (event as InputEvent).inputType);
	beforeInput = undefined; edited();
});
editor.addEventListener('blur', () => { if (cfg) history().separate(); });
editor.addEventListener('keydown', event => { if (event.key === 'Tab') { event.preventDefault(); insertText('\t'); } });
function insertText(text: string): void {
	const before = textState(); editor.setRangeText(text, editor.selectionStart, editor.selectionEnd, 'end');
	history().record(before, textState(), 'insertFromPaste'); edited();
}
function undo(redo: boolean): void {
	if (busy || !editing) return;
	if (state.engine === 'neovim') { enqueueNative(() => api.NativeUndo(redo)); return; }
	const entry = redo ? history().redo() : history().undo(); if (!entry) return;
	editor.value = entry.text; editor.setSelectionRange(entry.start, entry.end); edited(); updateUndoRedo(); editor.focus();
}
element('undo').onclick = () => undo(false); element('redo').onclick = () => undo(true);
const newDialog = element<HTMLDialogElement>('new-dialog');
element('new').onclick = () => newDialog.showModal();
element('welcome-new').onclick = () => newDialog.showModal();
element('welcome-open').onclick = () => element('open').click();
element('welcome-recovery').onclick = () => element('recovery').click();
// 種類のボタンを押すと、そのまま閲覧モードで新しい文書を作成します。
for (const button of newDialog.querySelectorAll<HTMLButtonElement>('[data-new-kind]')) {
	button.onclick = () => {
		const kind = button.dataset.newKind!;
		newDialog.close();
		void action(async () => { await flush(); if (await api.NewDocument(kind)) await reload(true); });
	};
}
element('open').onclick = () => void action(async () => { await flush(); if (await api.Open('')) await reload(); });
element('save').onclick = () => void save(false); element('save-as').onclick = () => void save(true);
async function save(as: boolean): Promise<void> {
	if(!state?.id) return;
	await action(async () => {
		await flush();
		const single = state.singleMarkdown;
		if (await api.Save(as)) {
			await refreshSidebar();
			status(single ? (as ? 'MDZとして保存しました' : 'Markdownを保存しました') : '保存しました');
		}
	});
}
async function insertImage(name: string): Promise<void> {
	if (!name) return;
	const up = '../'.repeat(current.split('/').length - 1);
	const reference = (up + name).split('/').map(encodeURIComponent).join('/');
	const text = `![画像](${reference})`;
	if (state.engine === 'neovim') await api.NativePaste(text); else insertText(text);
	await refreshSidebar(); await render();
}
element('image').onclick = () => void action(async () => { await insertImage(await api.AddImage()); });
async function clipboardImage(file: File): Promise<void> {
	if (file.size > 32 * 1024 * 1024) throw new Error('画像は32MiB以下にしてください');
	const data = new Uint8Array(await file.arrayBuffer()); let binary = '';
	for (let i = 0; i < data.length; i += 32768) binary += String.fromCharCode(...data.subarray(i, i + 32768));
	await insertImage(await api.StoreImage(btoa(binary)));
}
for (const target of [editor, nativeInput]) target.addEventListener('paste', event => {
	if (busy || !editing) { event.preventDefault(); return; }
	const image = Array.from(event.clipboardData?.items || []).find(item => item.type.startsWith('image/'))?.getAsFile();
	if (image && state.singleMarkdown) {
		event.preventDefault();
		status('単一Markdownでは画像を同梱できません。MDZとして保存してから追加してください', true);
		return;
	}
	if (image) { event.preventDefault(); void action(() => clipboardImage(image)); return; }
	if (target === nativeInput) { event.preventDefault(); const text = event.clipboardData?.getData('text/plain') || ''; enqueueNative(() => api.NativePaste(text)); }
});
for (const mode of ['edit', 'split', 'view']) element(`${mode}-mode`).onclick = () => {
	element('panes').className = mode;
	for (const other of ['edit','split','view']) {
		const button = element<HTMLButtonElement>(`${other}-mode`);
		const selected = other === mode;
		button.classList.toggle('selected', selected);
		button.setAttribute('aria-pressed', String(selected));
	}
	native.resize();
};
const dialog = element<HTMLDialogElement>('page-dialog');
element('add-page').onclick = () => dialog.showModal();
dialog.addEventListener('close', () => {
	if (dialog.returnValue !== 'add') return;
	let name = element<HTMLInputElement>('page-name').value.trim();
	if(name && !name.endsWith('/') && !name.split('/').pop()!.includes('.')) name += '.md';
	void action(async () => { await flush(); await api.AddPage(name); await selectPage(name); status('ページを追加しました'); });
});
async function convertDocumentMode(target: 'slides' | 'document'): Promise<void> {
	if (!editing) return;
	const toSlides = target === 'slides';
	const message = toSlides
		? '各Markdownを --- で分割してスライドへ変換します。\n現在の文書構成が変更されます。続けますか？'
		: 'スライドを通常MDZへ変換します。\n変換元の情報があるページは可能な範囲で復元します。続けますか？';
	if (!confirm(message)) return;
	await action(async () => {
		await flush();
		await api.ConvertDocumentMode(target);
		await reload(true);
		status(toSlides ? 'スライドモードへ変換しました。保存すると確定します。' : '通常MDZへ変換しました。保存すると確定します。');
	});
}
element('document-to-slides').onclick = () => void convertDocumentMode('slides');
element('slides-to-document').onclick = () => void convertDocumentMode('document');
window.addEventListener('keydown', event => {
	if (!(event.ctrlKey || event.metaKey)) return;
	if (event.key.toLowerCase() === 's') { event.preventDefault(); void save(event.shiftKey); }
	if (event.key.toLowerCase() === 'o' && event.target !== nativeInput) { event.preventDefault(); element('open').click(); }
	if (state?.engine === 'builtin' && ['z','y'].includes(event.key.toLowerCase()) && event.target === editor) { event.preventDefault(); undo(event.shiftKey || event.key.toLowerCase() === 'y'); }
});
const settingsDialog = element<HTMLDialogElement>('settings-dialog');
element('settings').onclick = () => {
	element('settings-error').textContent = '';
	detectedInitPath = '';
	for (const [key, value] of Object.entries(cfg)) {
		const control = settingsDialog.querySelector<HTMLInputElement | HTMLSelectElement>(`[name="${key}"]`)!;
		if (typeof value === 'boolean') (control as HTMLInputElement).checked = value; else control.value = String(value);
	}
	settingsDialog.showModal();void checkDependencies();
};
element('choose-nvim').onclick = () => void action(async () => { const name = await api.ChooseExecutable(); if (name) {element<HTMLInputElement>('nvim-path').value = name;void checkDependencies();} });
element('choose-mdbook').onclick = () => void action(async () => { const p = await api.ChooseMdbook(); if(p) {element<HTMLInputElement>('mdbook-path').value=p;void checkDependencies();} });
element('settings-install-mdbook').onclick = () => void action(async () => {
	status('wingetでmdBookをインストールしています…');
	const installed = await api.InstallMdbook();
	cfg.mdbookPath = installed; cfg.mdbookDeclined = false; await api.Configure(cfg); // 導入後は拒否を解除する。
	element<HTMLInputElement>('mdbook-path').value = installed;
	settingsDialog.querySelector<HTMLInputElement>('[name="mdbookDeclined"]')!.checked = false;
	void checkDependencies();
	if (state.id) { bookFallback = false; await refreshBook(); }
	status('mdBookを導入しました。');
});
element('choose-init').onclick = () => void action(async () => {
	const name = await api.ChooseInit();
	if (!name) return;
	element<HTMLInputElement>('init-path').value = name;
	settingsDialog.querySelector<HTMLSelectElement>('[name="initMode"]')!.value = 'custom';
	void checkDependencies();
});
element<HTMLInputElement>('init-path').addEventListener('input', event => {
	if ((event.currentTarget as HTMLInputElement).value.trim()) {
		settingsDialog.querySelector<HTMLSelectElement>('[name="initMode"]')!.value = 'custom';
	}
});
element('settings-save').onclick = () => {
	if (!element<HTMLFormElement>('settings-form').reportValidity()) return;
	const next = { ...cfg };
	for (const [key, value] of Object.entries(cfg)) {
		const control = settingsDialog.querySelector<HTMLInputElement | HTMLSelectElement>(`[name="${key}"]`)!;
		(next as unknown as Record<string, unknown>)[key] = typeof value === 'boolean' ? (control as HTMLInputElement).checked : typeof value === 'number' ? Number(control.value) : control.value;
	}
	void action(async () => {
		await flush();
		const keepEngine = state?.engine;
		try { await api.Configure(next); } catch (error) { element('settings-error').textContent = String(error); throw error; }
		cfg = next; autoDeadline = 0; applyTheme();
		for (const h of histories.values()) h.setLimit(cfg.undoLevels);
		if(editing && keepEngine === 'neovim') await api.StartNative(); await refreshSidebar(); applyEditor();
		if (state.engine === 'neovim') {
			await api.NativeOpen(current);
			await api.NativeScroll(scrollSyncRatio);
		} else if(state.id) editor.value = await api.Text(current);
		await render(); if(state.id) await refreshBook();
		requestAnimationFrame(() => syncScroll('state', scrollSyncRatio, true));
		settingsDialog.close(); status('設定を保存しました');
	});
};
element('data-folder').onclick = () => void api.OpenDataFolder();
async function showRecoveries(): Promise<void> {
	const items = await api.Recoveries(); if (!items.length) { status('復旧できる作業データはありません'); return; }
	const list = element('recovery-list'); list.replaceChildren();
	for (const item of items) {
		const button = document.createElement('button'); button.textContent = `${item.filename || '新しい文書'} — ${new Date(item.updated).toLocaleString()}`;
		button.onclick = () => void action(async () => { await flush(); if (await api.Recover(item.id)) { element<HTMLDialogElement>('recovery-dialog').close(); await reload(); status('作業データを復旧しました。内容を確認して保存してください'); } }); list.append(button);
	}
	element<HTMLDialogElement>('recovery-dialog').showModal();
}
element('recovery').onclick = () => void showRecoveries().catch(error => status(String(error), true));
function markdownDropTarget(x: number, y: number): {target: string; after: boolean} | undefined {
	if (!state?.id || state.singleMarkdown) return undefined;
	const hit = document.elementFromPoint(x, y) as HTMLElement | null;
	const nav = element('pages');
	if (!hit || (hit !== nav && !nav.contains(hit))) return undefined;
	if (state.mode === 'slides') {
		const card = hit.closest<HTMLElement>('.slide-card');
		if (!card) return {target:'', after:true};
		const bounds = card.getBoundingClientRect();
		return {target:card.dataset.slide || '', after:y >= bounds.top + bounds.height / 2};
	}
	if (bookInfo?.present) {
		const item = hit.closest<HTMLElement>('[data-toc-id]');
		if (!item) return {target:'', after:true};
		const bounds = item.getBoundingClientRect();
		return {target:item.dataset.tocId || '', after:y >= bounds.top + bounds.height / 2};
	}
	const page = hit.closest<HTMLElement>('button[data-page]');
	const pageName = page?.dataset.page || '';
	if (!page || !state.pages.includes(pageName)) return {target:'', after:true};
	if (page.classList.contains('markdown-heading')) return {target:pageName, after:true};
	const bounds = page.getBoundingClientRect();
	return {target:pageName, after:y >= bounds.top + bounds.height / 2};
}
function naturalDroppedDocuments(paths: string[]): string[] {
	return [...paths].sort((a,b) => (a.split(/[\\/]/).pop() || a).localeCompare(b.split(/[\\/]/).pop() || b, undefined, {numeric:true,sensitivity:'base'}));
}
function isExternalFileDrag(event: DragEvent): boolean {
	return !!event.dataTransfer?.types.includes('Files');
}
function setExternalFileDrag(active: boolean): void {
	document.body.classList.toggle('external-file-drag', active);
}
// iframe上でWebView標準のファイルドロップが処理されないよう、外部ファイルのドラッグ中は親画面で受け止めます。
document.addEventListener('dragenter', event => {
	if (!isExternalFileDrag(event)) return;
	event.preventDefault();
	setExternalFileDrag(true);
}, true);
document.addEventListener('dragover', event => {
	if (!isExternalFileDrag(event)) return;
	event.preventDefault();
	if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy';
	setExternalFileDrag(true);
}, true);
document.addEventListener('drop', event => {
	if (!isExternalFileDrag(event)) return;
	event.preventDefault();
	setExternalFileDrag(false);
}, true);
document.addEventListener('dragleave', event => {
	if (!isExternalFileDrag(event)) return;
	if (event.relatedTarget === null) setExternalFileDrag(false);
}, true);
window.addEventListener('blur', () => setExternalFileDrag(false));
const iframeFileDropDocuments = new WeakSet<Document>();
function installIframeFileDropBridge(frame: HTMLIFrameElement): void {
	let frameDocument: Document | null;
	try { frameDocument = frame.contentDocument; } catch { return; }
	if (!frameDocument || iframeFileDropDocuments.has(frameDocument)) return;
	iframeFileDropDocuments.add(frameDocument);
	const captureDrag = (event: DragEvent): void => {
		if (!isExternalFileDrag(event)) return;
		event.preventDefault();
		if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy';
		setExternalFileDrag(true);
	};
	frameDocument.addEventListener('dragenter', captureDrag, true);
	frameDocument.addEventListener('dragover', captureDrag, true);
	frameDocument.addEventListener('drop', event => {
		if (!isExternalFileDrag(event)) return;
		event.preventDefault();
		event.stopPropagation();
		setExternalFileDrag(false);
		const files = [...(event.dataTransfer?.files || [])];
		if (!files.length || !window.runtime.ResolveFilePaths) return;
		const bounds = frame.getBoundingClientRect();
		window.runtime.ResolveFilePaths(bounds.left + event.clientX, bounds.top + event.clientY, files);
	}, true);
}
document.addEventListener('load', event => {
	if (event.target instanceof HTMLIFrameElement) installIframeFileDropBridge(event.target);
}, true);
document.querySelectorAll<HTMLIFrameElement>('iframe').forEach(installIframeFileDropBridge);
// WebViewのドロップ処理を登録します。文書一覧へのMarkdownは取り込み、それ以外の文書は別ウィンドウで開きます。
window.runtime.OnFileDrop((x, y, paths) => {
	if (!paths.length) return;
	const documents = naturalDroppedDocuments(paths.filter(p=>/\.(md|markdown|mdz)$/i.test(p)));
	if (!state?.id && documents.length) {
		void action(async () => {
			if (await api.Open(documents[0])) {
				await reload();
				status(`${documents[0].split(/[\\/]/).pop()} を開きました`);
			}
			for (const name of documents.slice(1)) await api.OpenInNewWindow(name);
		});
		return;
	}
	const markdowns = naturalDroppedDocuments(paths.filter(p=>/\.(md|markdown)$/i.test(p)));
	const listTarget = markdowns.length ? markdownDropTarget(x, y) : undefined;
	if (listTarget) {
		void action(async () => {
			await flush();
			const imported = await api.ImportMarkdownFiles(markdowns, listTarget.target, listTarget.after);
			if (imported.length) {
				await refreshSidebar();
				if (bookInfo?.present) await refreshBook();
				status(`${imported.length}個のMarkdownを取り込みました`);
			}
		});
		return;
	}
	const hit = document.elementFromPoint(x, y) as HTMLElement | null;
	const overPageList = !!hit && (hit === element('pages') || element('pages').contains(hit));
	if (overPageList && state?.id && !state.singleMarkdown) return;
	if (editing && state?.mode === 'slides' && paths.every(p=>/\.(png|jpe?g|gif|webp|svg)$/i.test(p))) {
		void action(async()=>{ for (const path of paths) await insertImage(await api.ImportImage(path)); });
		return;
	}
	if (!documents.length) return;
	void action(async () => {
		await flush();
		for (const name of documents) await api.OpenInNewWindow(name);
		status(`${documents.length}個の文書を別ウィンドウで開きました`);
	});
}, false);
window.runtime.EventsOn('native-redraw', events => native.redraw(events));
window.runtime.EventsOn('native-scroll', ratio => {
	if (busy) return;
	const value = Number(ratio);
	if (Number.isFinite(value)) syncScroll('native', value);
});
window.runtime.EventsOn('native-save', () => { nativeSaveRequested = true; });
window.runtime.EventsOn('native-changed', () => { if (state) { state.dirty = true; refreshTitle(); } });
window.runtime.EventsOn('app-error', error => status(String(error), true));

void action(async () => {
	cfg = await api.Settings(); applyTheme(); await reload();
	const name = await api.Initial(); if (name && await api.Open(name)) await reload();
	if (!name) await showRecoveries();
});
let polling = false;
window.setInterval(() => {
	if (busy || polling || !state?.id || !editing) return;
	if (nativeSaveRequested) { nativeSaveRequested = false; void save(false); return; }
	polling = true;
	activePoll = (async () => {
	try {
		if (state.engine === 'neovim') {
			await nativePending;
			const value = await api.NativePoll();
			if (value.changed) {
				nativeCanUndo = value.canUndo; nativeCanRedo = value.canRedo;
				current = value.name; editor.value = value.text; await refreshSidebar(); await render(); updateUndoRedo();
			}
		}
	} catch (error) { await refreshSidebar(); applyEditor(); if (state.engine === 'builtin') { editor.value = await api.Text(current); await render(); } status(String(error), true); }
	finally { polling = false; }
	})();
}, 400);
window.setInterval(() => {
	if (!state?.id || busy || polling || composing || nativeInput.classList.contains('composing') || !cfg?.autoSave || Date.now() < autoDeadline) return;
	autoDeadline = Date.now() + cfg.autoSaveSeconds * 1000;
	void action(async () => { await flush(); if (await api.AutoSave()) { await refreshSidebar(); status('自動保存しました'); } });
}, 1000);

// mdBookの目次はファイル名ではなく、章の表示名と階層を使って操作します。
function renderContents(nav: HTMLElement): void {
	if(!contents) return;
	element('toc-edit').textContent=tocEditing?'目次の編集を終える':'目次を編集';
	const levels: number[]=[];
	for(const entry of contents.entries) {
		if(entry.kind==='raw'||entry.kind==='heading') continue;
		if(entry.kind==='separator') { nav.append(document.createElement('hr')); continue; }
		if(entry.kind==='part'&&!tocEditing) {
			levels.length=0;
			const heading=document.createElement('h3');
			heading.className='toc-part';
			heading.dataset.tocId=String(entry.id);
			heading.style.padding='6px 12px';
			heading.textContent=entry.title;
			nav.append(heading);
			continue;
		}
		const b=document.createElement('button'); b.dataset.tocId=String(entry.id); b.dataset.page=entry.name;
		b.className=entry.kind==='part'?'toc-part':'toc-page';
		b.style.paddingLeft=`${12+entry.depth*17}px`;
		let number='';
		if(entry.kind==='part') levels.length=0;
		if(entry.kind==='chapter') { levels.length=entry.depth+1; levels[entry.depth]=(levels[entry.depth]||0)+1; number=levels.map(n=>n||1).join('.')+'. '; }
		b.textContent=number+entry.title+(entry.missing?'（ファイルなし）':entry.kind==='chapter'&&!entry.target?'（下書き）':'');
		const currentPage=entry.name===current;
		b.classList.toggle('selected',tocEditing?entry.id===tocSelected:currentPage);
		b.classList.toggle('current-page',currentPage);
		if(currentPage) b.setAttribute('aria-current','page');
		b.title=entry.name ? `ページ: ${entry.name}` : entry.kind==='part'?'部タイトル':'下書き';
		b.onclick=()=>void action(async()=>{await flush();tocSelected=entry.id;selectedTocName=entry.name;if(!tocEditing&&entry.name&&!entry.missing) await selectPage(entry.name);else await refreshSidebar();});
		nav.append(b);
	}
	const unlisted=element('unlisted-pages');unlisted.replaceChildren();
	element('book-unlisted').hidden=contents.unlisted.length===0;
	for(const name of contents.unlisted) {
		const row=document.createElement('div');const b=document.createElement('button');b.textContent=name.split('/').pop()!;b.onclick=()=>void action(async()=>{await flush();await selectPage(name);});row.append(b);
		if(editing&&tocEditing) {const add=document.createElement('button');add.textContent='目次へ追加';add.onclick=()=>void action(()=>mutateContents('attach',name));row.append(add);}
		unlisted.append(row);
	}
	for(const id of ['toc-undo','toc-redo']) element(id).setAttribute('aria-disabled',String(id==='toc-undo'?!contents.canUndo:!contents.canRedo));
}
async function mutateContents(operation: string,value=''): Promise<void> {
	if(!editing||!contents) return;
	await flush();
	const oldNames=new Set(contents.entries.map(e=>e.name));
	try { contents=await api.ChangeContents(contents.revision,tocSelected,operation,value); }
	catch(error) {await refreshSidebar();throw error;}
	if(operation==='add'||operation==='attach') {
		const added=contents.entries.find(e=>e.name&&!oldNames.has(e.name));
		if(added) {
			tocSelected=added.id;selectedTocName=added.name;
			if(!tocEditing) await selectPage(added.name);
		}
	} else {tocSelected=contents.entries.find(e=>e.name&&e.name===selectedTocName)?.id ?? -1;}
	if(tocEditing && current===bookSummaryName() && state.engine==='builtin') {
		editor.value=await api.Text(current);changed=false;beforeInput=undefined;histories.delete(current);await render();
	}
	await refreshSidebar();status('目次を更新しました');
}
element('toc-edit').onclick=()=>void action(async()=>{
	await flush();
	if (!tocEditing) {
		tocReturnPage = state.pages.includes(current) ? current : state.entry;
		tocSelected = contents?.entries.find(e=>e.name===current)?.id ?? -1;
		tocEditing = true;
		await selectPage(bookSummaryName());
		status('左の目次GUIと右のSUMMARY.mdをどちらからでも編集できます');
		return;
	}
	tocEditing = false;
	contents = await api.Contents();
	const target = state.pages.includes(tocReturnPage) ? tocReturnPage : contents.entries.find(e=>e.name&&!e.missing)?.name || state.entry;
	tocReturnPage = '';
	await selectPage(target);
	status('目次の編集を終了しました');
});
const tocDialog=element<HTMLDialogElement>('toc-dialog');
function openTocDialog(operation: string): void {
	tocOperation=operation;element('toc-dialog-title').textContent=operation==='add'?'ページを追加':operation==='part'?'部タイトルを追加':'目次の表示名を変更';
	element<HTMLInputElement>('toc-name').value=operation==='rename'?contents?.entries.find(e=>e.id===tocSelected)?.title||'':'';
	element('toc-error').textContent='';tocDialog.showModal();
}
element('toc-add').onclick=()=>openTocDialog('add');element('toc-part').onclick=()=>openTocDialog('part');
element('toc-cancel').onclick=()=>tocDialog.close();
element('toc-undo').onclick=()=>{if(contents?.canUndo) void action(()=>mutateContents('undo'));};
element('toc-redo').onclick=()=>{if(contents?.canRedo) void action(()=>mutateContents('redo'));};
for(const b of document.querySelectorAll<HTMLButtonElement>('[data-toc-op]')) b.onclick=()=>{
	const op=b.dataset.tocOp!;if(op==='rename'){openTocDialog(op);return;}
	if(op==='remove'&&!confirm('この項目と子ページを目次から外しますか？\nページ本文は保持されます。')) return;
	void action(()=>mutateContents(op));
};
element<HTMLFormElement>('toc-form').onsubmit=event=>{event.preventDefault();void action(async()=>{try{await mutateContents(tocOperation,element<HTMLInputElement>('toc-name').value);tocDialog.close();}catch(error){element('toc-error').textContent=String(error);throw error;}});};
const bookConfigDialog=element<HTMLDialogElement>('book-config-dialog');
const bookConfigText=element<HTMLTextAreaElement>('book-config-text');
element('book-config').onclick=()=>void action(async()=>{
	await flush();const config=await api.GetBookConfiguration();configRevision=config.revision;bookConfigText.value=config.text;bookConfigText.readOnly=!editing;
	element('book-config-edit').hidden=editing;element('book-config-save').hidden=!editing;element('book-config-error').textContent='';bookConfigDialog.showModal();
});
element('book-config-edit').onclick=()=>{bookConfigText.readOnly=false;element('book-config-edit').hidden=true;element('book-config-save').hidden=false;bookConfigText.focus();};
element('book-config-cancel').onclick=()=>bookConfigDialog.close();
element<HTMLFormElement>('book-config-form').onsubmit=event=>{
	event.preventDefault();if(bookConfigText.readOnly)return;
	void action(async()=>{try{await api.SaveBookConfiguration(configRevision,bookConfigText.value);bookConfigDialog.close();bookInfo=await api.BookStatus();await render();await refreshBook();status('本の設定を適用しました。MDZの保存で確定します。');}catch(error){element('book-config-error').textContent=String(error);throw error;}});
};

const unsavedDialog = element<HTMLDialogElement>('unsaved-dialog');
window.runtime.EventsOn('confirm-unsaved', () => {
	unsavedDialog.querySelectorAll('button').forEach(button => button.disabled = false);
	unsavedDialog.showModal();
	element('unsaved-save').focus();
});
async function resolveUnsaved(choice: string): Promise<void> {
	unsavedDialog.querySelectorAll('button').forEach(button => button.disabled = true);
	try {
		if (choice === 'save') {
			await activePoll; await markPending; await nativePending; await flush();
		}
		unsavedDialog.close();
		await api.ResolveUnsaved(choice);
	} catch (error) {
		unsavedDialog.close();
		status(String(error), true);
		await api.ResolveUnsaved('cancel');
	}
}
element('unsaved-save').onclick = () => void resolveUnsaved('save');
element('unsaved-discard').onclick = () => void resolveUnsaved('discard');
element('unsaved-cancel').onclick = () => void resolveUnsaved('cancel');
unsavedDialog.addEventListener('cancel', event => { event.preventDefault(); void resolveUnsaved('cancel'); });

const bookTitleDialog = element<HTMLDialogElement>('book-title-dialog');
let bookTitleRevision = '';
element('book-title-edit').onclick = () => void action(async () => {
	if (!editing) return;
	await flush();
	const config = await api.GetBookConfiguration();
	bookTitleRevision = config.revision;
	bookInfo = await api.BookStatus();
	element<HTMLInputElement>('book-title-input').value = bookInfo.title;
	element('book-title-error').textContent = '';
	bookTitleDialog.showModal();
	element<HTMLInputElement>('book-title-input').select();
});
element('book-title-cancel').onclick = () => bookTitleDialog.close();
element<HTMLFormElement>('book-title-form').onsubmit = event => {
	event.preventDefault();
	if (!editing) return;
	void action(async () => {
		try {
			await api.RenameBook(bookTitleRevision, element<HTMLInputElement>('book-title-input').value);
			bookTitleDialog.close();
			await refreshBook();
			status('本のタイトルを変更しました');
		} catch (error) {
			element('book-title-error').textContent = String(error);
			throw error;
		}
	});
};

// スライドの構成と本文を分離し、編集・発表で同じ描画を使います。
function selectedSlide(): Slide | undefined { return slidesInfo?.deck.slides.find(s=>s.file===current); }
interface SlideTypographyValues { fontFamily: string; marginX: number; marginY: number; body: number; h1: number; h2: number; h3: number; h4: number; h5: number }
function slideFontStack(value?: string): string {
	switch(value) {
	case 'gothic': return 'Yu Gothic UI, Yu Gothic, Meiryo, sans-serif';
	case 'mincho': return 'Yu Mincho, Hiragino Mincho ProN, Noto Serif JP, serif';
	case 'monospace': return 'Cascadia Mono, Consolas, monospace';
	default: return 'Segoe UI, Yu Gothic UI, Yu Gothic, Meiryo, sans-serif';
	}
}
function slideTypographyValues(deck: SlideDeck, slide: Slide): SlideTypographyValues {
	const body = deck.bodyFontSize || slide.fontSize || 32;
	const h5 = deck.h5FontSize || Math.min(72,Math.max(14,Math.round(body*.9)));
	const h4 = deck.h4FontSize || Math.min(78,Math.max(h5+2,Math.round(body*1.0)));
	const h3 = deck.h3FontSize || Math.min(84,Math.max(h4+2,Math.round(body*1.12)));
	const h2 = deck.h2FontSize || Math.min(90,Math.max(h3+2,Math.round(body*1.3)));
	const h1 = deck.h1FontSize || Math.min(96,Math.max(h2+2,Math.round(body*1.65)));
	return {fontFamily:deck.fontFamily || 'system',marginX:deck.contentMarginX || 60,marginY:deck.contentMarginY || 48,body,h1,h2,h3,h4,h5};
}
function frameTypography(deck: SlideDeck): Record<string, unknown> {
	return {contentMarginX:deck.contentMarginX || 60,contentMarginY:deck.contentMarginY || 48,fontFamily:deck.fontFamily || '',bodyFontSize:deck.bodyFontSize || 0,h1FontSize:deck.h1FontSize || 0,h2FontSize:deck.h2FontSize || 0,h3FontSize:deck.h3FontSize || 0,h4FontSize:deck.h4FontSize || 0,h5FontSize:deck.h5FontSize || 0};
}
function slideTypographyStyle(deck: SlideDeck, slide: Slide): string {
	const parts=[`font-size:${deck.bodyFontSize || slide.fontSize}px`,`--slide-margin-x:${deck.contentMarginX || 60}px`,`--slide-margin-y:${deck.contentMarginY || 48}px`];
	if(deck.fontFamily) parts.push(`font-family:${slideFontStack(deck.fontFamily)}`);
	for(const level of [1,2,3,4,5]) {
		const size=deck[`h${level}FontSize` as keyof SlideDeck];
		if(typeof size==='number' && size>0) parts.push(`--slide-h${level}-size:${size}px`);
	}
	return parts.join(';');
}
function refreshSlideTypography(): void {
	const panel=element<HTMLDetailsElement>('slide-typography');
	const slide=selectedSlide();
	panel.hidden=!editing || state?.mode!=='slides' || !slidesInfo || !slide;
	if(panel.hidden || !slide || !slidesInfo)return;
	if(panel.contains(document.activeElement) && (document.activeElement as HTMLElement).matches('input,select'))return;
	const value=slideTypographyValues(slidesInfo.deck,slide);
	element<HTMLSelectElement>('slide-font-family').value=value.fontFamily;
	element<HTMLInputElement>('slide-margin-x').value=String(value.marginX);
	element<HTMLInputElement>('slide-margin-y').value=String(value.marginY);
	for(const key of ['body','h1','h2','h3','h4','h5'] as const) element<HTMLInputElement>(`slide-${key}-size`).value=String(value[key]);
	element('slide-typography-error').textContent='';
}
function sendFrame(frame: HTMLIFrameElement, data: Record<string, unknown>): void {
	delete frame.dataset.rendered; framePayloads.set(frame, data); frame.contentWindow?.postMessage(data, location.origin);
}
async function slideHTML(slide: Slide, text?: string): Promise<string> {
	const html = await api.RenderSlide(text ?? await api.Text(slide.file), slide.layout);
	const doc = new DOMParser().parseFromString(html, 'text/html');
	for (const image of doc.querySelectorAll('img')) {
		const target = resolveLink(image.getAttribute('src') || '', slide.file);
		if (target) image.src = `${location.origin}/bundle/${target.path.split('/').map(encodeURIComponent).join('/')}`;
		else { image.removeAttribute('src'); image.alt += '（外部画像は非表示）'; }
	}
	const deck=slidesInfo?.deck;
	if(deck?.fontFamily) for(const node of doc.body.querySelectorAll<HTMLElement>('*')) node.style.fontFamily=slideFontStack(deck.fontFamily);
	if(deck) for(const level of [1,2,3,4,5]) {
		const size=deck[`h${level}FontSize` as keyof SlideDeck];
		if(typeof size==='number' && size>0) for(const heading of doc.body.querySelectorAll<HTMLElement>(`h${level}`)) heading.style.fontSize=`${size}px`;
	}
	return doc.body.innerHTML;
}
async function renderSlidePreview(): Promise<void> {
	const slide = selectedSlide(); if (!slide || !slidesInfo) return;
	const ticket = ++renderID;
	const deck = slidesInfo.deck;
	const html = await slideHTML(slide, editor.value);
	if (ticket!==renderID || current!==slide.file || state.mode!=='slides') return;
	preview.hidden=true; element('book-preview').hidden=true; slideFrame.hidden=false;
	element('slide-overflow').hidden=true; refreshSlideTypography();
	sendFrame(slideFrame, {type:'mdz-slide-render', token:ticket, slides:[{...slide, html}], theme:deck.theme, aspect:deck.aspect,marginColor:deck.marginColor||'#ffffff', ...frameTypography(deck), index:0, presentation:false, editorPreview:editing, previewNavigation:!editing, debugOutline:slideDebugOutline});
	const thumbnail = document.querySelector<HTMLIFrameElement>('.slide-card.selected iframe');
	if (thumbnail) setThumbnail(thumbnail, slide, html, deck);
}
function setThumbnail(frame: HTMLIFrameElement, slide: Slide, html: string, deck: SlideDeck): void {
	// スクリプトを許可しない小さなプレビューを同じスタイルで表示します。
	const height = deck.aspect==='4:3'?720:540;
	frame.style.aspectRatio = deck.aspect.replace(':','/');
	const width = frame.clientWidth || 180;
	const scale = width/960;
	const bg = /^#[0-9a-f]{6}$/i.test(slide.background || '') ? slide.background : '';
	const key=JSON.stringify([html,slide.layout,slide.fontSize,bg,deck.theme,deck.aspect,deck.contentMarginX,deck.contentMarginY,deck.fontFamily,deck.bodyFontSize,deck.h1FontSize,deck.h2FontSize,deck.h3FontSize,deck.h4FontSize,deck.h5FontSize,width]);
	if(thumbnailKeys.get(frame)===key)return;
	thumbnailKeys.set(frame,key);
	frame.srcdoc = `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${location.origin} 'unsafe-inline'; img-src ${location.origin}; base-uri 'none'"><link rel="stylesheet" href="${location.origin}/vendor/reveal.css"><link rel="stylesheet" href="${location.origin}/slides.css"><style>html,body{width:100%;height:100%;background:transparent}.reveal{width:960px;height:${height}px;transform:scale(${scale});transform-origin:0 0}.reveal .slides{position:relative;width:960px;height:${height}px;left:0;top:0;transform:none}.reveal .slides>section{display:block;position:relative;top:0;left:0}</style></head><body><div class="reveal"><div class="slides"><section data-theme="${deck.theme}" data-layout="${slide.layout}" style="${slideTypographyStyle(deck,slide)};${bg?'background-color:'+bg:''}"><div class="slide-content">${html}</div></section></div></div></body></html>`;
}
let thumbnailObserver: IntersectionObserver | undefined;
let slideListKey = '';
const thumbnailKeys = new WeakMap<HTMLIFrameElement,string>();
function renderSlideList(nav: HTMLElement): void {
	if (!slidesInfo) return;
	const deck = slidesInfo.deck;
	element('slides-title').textContent=deck.title;
	element('slide-position').textContent=`${Math.max(0,deck.slides.findIndex(s=>s.file===current))+1} / ${deck.slides.length}`;
	const key = JSON.stringify([state.id,deck,editing]);
	if(key===slideListKey) {
		nav.querySelectorAll<HTMLElement>('.slide-card').forEach(card=>card.classList.toggle('selected',deck.slides.find(s=>s.id===card.dataset.slide)?.file===current));
		return;
	}
	slideListKey=key;nav.replaceChildren();
	const epoch = ++thumbnailEpoch;
	thumbnailObserver?.disconnect();
	thumbnailObserver = new IntersectionObserver(entries => {
		for (const entry of entries) {
			if (!entry.isIntersecting) continue;
			thumbnailObserver?.unobserve(entry.target);
			const frame = entry.target as HTMLIFrameElement;
			const slide = deck.slides.find(s=>s.id===frame.dataset.slide)!;
			void slideHTML(slide, slide.file===current?editor.value:undefined).then(html=>{ if(epoch===thumbnailEpoch && frame.isConnected) setThumbnail(frame, slide, html, deck); }).catch(error=>status(String(error),true));
		}
	}, {root:element('sidebar'), rootMargin:'80px'});
	for (const [index, slide] of deck.slides.entries()) {
		const card = document.createElement('button'); card.className='slide-card'; card.dataset.slide=slide.id;
		card.classList.toggle('selected', slide.file===current); card.draggable=editing;
		card.title=slide.title; card.setAttribute('aria-label', `${index+1}. ${slide.title}`);
		const thumbnail = document.createElement('iframe'); thumbnail.sandbox.add('allow-same-origin'); thumbnail.tabIndex=-1; thumbnail.title=slide.title; thumbnail.dataset.slide=slide.id; thumbnail.setAttribute('aria-hidden','true');
		const label = document.createElement('span'); label.className='slide-card-label'; label.textContent=`${index+1}. ${slide.title}`;
		card.append(thumbnail,label);
		card.onclick=()=>void action(async()=>{await flush();await selectPage(slide.file);});
		card.oncontextmenu=event=>{event.preventDefault();event.stopPropagation();if(!busy)void action(async()=>{await flush();await selectPage(slide.file);showSlideMenu(event.clientX,event.clientY);});};
		card.ondragstart=event=>{event.dataTransfer?.setData('application/x-mdz-slide',slide.id);};
		const clear=()=>nav.querySelectorAll('.slide-insert-before,.slide-insert-after').forEach(n=>n.classList.remove('slide-insert-before','slide-insert-after'));
		card.ondragend=clear;
		card.ondragover=event=>{if(editing && event.dataTransfer?.types.includes('application/x-mdz-slide')){event.preventDefault();clear();const r=card.getBoundingClientRect();card.classList.add(event.clientY<r.top+r.height/2?'slide-insert-before':'slide-insert-after');}};
		card.ondragleave=()=>card.classList.remove('slide-insert-before','slide-insert-after');
		card.ondrop=event=>{clear();const id=event.dataTransfer?.getData('application/x-mdz-slide');if(editing&&id){event.preventDefault();event.stopPropagation();const r=card.getBoundingClientRect();void action(()=>mutateSlide(event.clientY<r.top+r.height/2?'move-before':'move-after',slide.id,id));}};

		nav.append(card); thumbnailObserver.observe(thumbnail);
	}
}
async function mutateSlide(operation: string, value='', id=selectedSlide()?.id || ''): Promise<void> {
	if (!editing || !slidesInfo) return;
	await flush();
	const oldFiles = new Set(slidesInfo.deck.slides.map(s=>s.file));
	const oldIndex = slidesInfo.deck.slides.findIndex(s=>s.file===current);
	try { slidesInfo=await api.ChangeSlide(slidesInfo.revision,id,operation,value); }
	catch(error) {await refreshSidebar();throw error;}
	const added = slidesInfo.deck.slides.find(s=>!oldFiles.has(s.file));
	const next = added?.file || (slidesInfo.deck.slides.some(s=>s.file===current)?current:slidesInfo.deck.slides[Math.min(Math.max(oldIndex,0),slidesInfo.deck.slides.length-1)].file);
	await selectPage(next);status('スライドの構成を更新しました');
}
for (const button of document.querySelectorAll<HTMLButtonElement>('[data-slide-op]')) button.onclick=()=>{
	const operation=button.dataset.slideOp!;
	if(operation==='remove' && !confirm('このスライドを一覧から削除しますか？\n構成の「元に戻す」で復元できます。')) return;
	void action(()=>mutateSlide(operation));
};
const splitCurrentSlide=()=>void action(()=>mutateSlide('split'));
element('slide-split').onclick=splitCurrentSlide;
element('slide-overflow-split').onclick=splitCurrentSlide;
element('slide-overflow-font').onclick=()=>{const panel=element<HTMLDetailsElement>('slide-typography');panel.open=true;element<HTMLInputElement>('slide-body-size').focus();};
element<HTMLButtonElement>('slide-debug-outline').onclick=()=>{slideDebugOutline=!slideDebugOutline;updateSlideDebugToggle();if(state?.mode==='slides'&&editing)void renderSlidePreview();};
function readSlideTypographyDeck(): SlideDeck | undefined {
	if(!editing||!slidesInfo)return;
	const number=(id:string)=>Number(element<HTMLInputElement>(id).value);
	const body=number('slide-body-size'),h1=number('slide-h1-size'),h2=number('slide-h2-size'),h3=number('slide-h3-size'),h4=number('slide-h4-size'),h5=number('slide-h5-size'),marginX=number('slide-margin-x'),marginY=number('slide-margin-y');
	const error=element('slide-typography-error');
	if(![body,h1,h2,h3,h4,h5,marginX,marginY].every(Number.isFinite)||body<12||body>64||[h1,h2,h3,h4,h5].some(size=>size<14||size>96)||marginX<8||marginX>180||marginY<8||marginY>180){error.textContent='本文は12〜64、見出しは14〜96、余白は8〜180pxで指定してください。';return;}
	if(!(h1>h2&&h2>h3&&h3>h4&&h4>h5)){error.textContent='見出しは H1 > H2 > H3 > H4 > H5 の大きさにしてください。';return;}
	error.textContent='';
	const deck=structuredClone(slidesInfo.deck);
	deck.contentMarginX=marginX;deck.contentMarginY=marginY;deck.fontFamily=element<HTMLSelectElement>('slide-font-family').value;deck.bodyFontSize=body;deck.h1FontSize=h1;deck.h2FontSize=h2;deck.h3FontSize=h3;deck.h4FontSize=h4;deck.h5FontSize=h5;
	return deck;
}
function updateSlideTypography(): void {
	const deck=readSlideTypographyDeck();
	if(!deck||!slidesInfo)return;
	slideTypographyDesired=deck;
	slideTypographyGeneration++;
	slidesInfo={...slidesInfo,deck};
	void renderSlidePreview();
	window.clearTimeout(slideTypographyTimer);
	slideTypographyTimer=window.setTimeout(()=>void flushSlideTypography(),180);
}
async function flushSlideTypography(force=false): Promise<void> {
	window.clearTimeout(slideTypographyTimer);
	slideTypographyTimer=0;
	if(busy&&!force){
		slideTypographyTimer=window.setTimeout(()=>void flushSlideTypography(),100);
		return;
	}
	const desired=slideTypographyDesired;
	if(!desired||!slidesInfo){await slideTypographyPending;return;}
	const generation=slideTypographyGeneration;
	slideTypographyDesired=undefined;
	slideTypographyPending=slideTypographyPending.catch(()=>{}).then(async()=>{
		if(!slidesInfo||state?.mode!=='slides')return;
		try {
			const saved=await api.ConfigureSlides(slidesInfo.revision,desired);
			if(slideTypographyGeneration===generation)slidesInfo=saved;
			else slidesInfo={...saved,deck:slidesInfo.deck};
			state=await api.State();
			refreshTitle();
			slideListKey='';
			renderSlideList(element('pages'));
			status('文字・ページ設定を更新しました');
		} catch(err) {
			element('slide-typography-error').textContent=String(err);
			status(String(err),true);
		}
	});
	await slideTypographyPending;
}
for(const id of ['slide-font-family','slide-margin-x','slide-margin-y','slide-body-size','slide-h1-size','slide-h2-size','slide-h3-size','slide-h4-size','slide-h5-size']){
	const control=element<HTMLInputElement|HTMLSelectElement>(id);
	control.addEventListener('input',updateSlideTypography);
	if(control instanceof HTMLSelectElement)control.addEventListener('change',updateSlideTypography);
}
function navigateSlide(offset: number): void {
	const slides=slidesInfo?.deck.slides; if(!slides) return;
	const index=slides.findIndex(s=>s.file===current)+offset;
	if(index>=0 && index<slides.length) void action(async()=>{await flush();await selectPage(slides[index].file);});
}
element('slide-prev').onclick=()=>navigateSlide(-1);element('slide-next').onclick=()=>navigateSlide(1);
element('slides-settings').onclick=()=>{
	if(!slidesInfo) return;
	slideSettings=structuredClone(slidesInfo);
	const deck=slideSettings.deck, slide=deck.slides.find(s=>s.file===current)!;
	for(const [id,value] of Object.entries({'deck-title':deck.title,'deck-theme':deck.theme,'deck-aspect':deck.aspect,'deck-margin':deck.marginColor||'#ffffff','slide-title':slide.title,'slide-layout':slide.layout,'slide-background':slide.background||'','slide-notes':slide.notes||''})) {
		const control=element<HTMLInputElement|HTMLSelectElement|HTMLTextAreaElement>(id); control.value=String(value); control.disabled=!editing;
	}
	element('slides-settings-save').hidden=!editing;element('slides-settings-error').textContent='';element<HTMLDialogElement>('slides-settings-dialog').showModal();
};
element('slides-settings-cancel').onclick=()=>element<HTMLDialogElement>('slides-settings-dialog').close();
element('slides-settings-form').onsubmit=event=>{
	event.preventDefault();if(!editing||!slideSettings)return;
	const value=(id:string)=>element<HTMLInputElement>(id).value;
	const deck=structuredClone(slideSettings.deck), slide=deck.slides.find(s=>s.file===current)!;
	deck.title=value('deck-title').trim();deck.theme=value('deck-theme');deck.aspect=value('deck-aspect');deck.marginColor=value('deck-margin');
	slide.title=value('slide-title').trim();slide.layout=value('slide-layout');slide.background=value('slide-background');slide.notes=value('slide-notes');
	void action(async()=>{
		await flush();
		try {slidesInfo=await api.ConfigureSlides(slideSettings!.revision,deck);}
		catch(error){element('slides-settings-error').textContent=String(error);throw error;}
		element<HTMLDialogElement>('slides-settings-dialog').close();await refreshSidebar();await render();status('スライドの設定を更新しました');
	});
};
element('slides-import').onclick=()=>{element('slides-import-error').textContent='';element<HTMLDialogElement>('slides-import-dialog').showModal();};
element('slides-import-cancel').onclick=()=>element<HTMLDialogElement>('slides-import-dialog').close();
element('slides-import-form').onsubmit=event=>{event.preventDefault();void action(async()=>{
	try{await mutateSlide('import',element<HTMLTextAreaElement>('slides-import-text').value);element<HTMLDialogElement>('slides-import-dialog').close();}
	catch(error){element('slides-import-error').textContent=String(error);throw error;}
});};
let presentationFocus: HTMLElement | null = null;
let presentationOwnsFullscreen = false;
function closePresentation(): void {
	const overlay=element('presentation');if(overlay.hidden)return;
	overlay.hidden=true;
	if(presentationOwnsFullscreen) { window.runtime.WindowUnfullscreen?.(); presentationOwnsFullscreen=false; }
	if(document.fullscreenElement===overlay) void document.exitFullscreen().catch(()=>{});
	framePayloads.delete(presentationFrame); ++presentationToken;
	presentationFocus?.focus({preventScroll:true});
	const slide=slidesInfo?.deck.slides[presentationIndex];
	if(slide && !busy && state?.mode==='slides') void action(async()=>{await selectPage(slide.file);});
}
function openPresentation(first: boolean): void {
	if(!slidesInfo || busy)return;
	presentationFocus=document.activeElement as HTMLElement;
	const overlay=element('presentation');overlay.hidden=false;overlay.focus();
	// デスクトップ版はタイトルバーも含めて全画面化し、終了時に元の状態へ戻します。
	const nativeFullscreen = window.runtime.WindowFullscreen && window.runtime.WindowUnfullscreen && window.runtime.WindowIsFullscreen;
	const fullscreen = nativeFullscreen ? window.runtime.WindowIsFullscreen!().then(active=>{
		if(!active && !overlay.hidden) { window.runtime.WindowFullscreen!(); presentationOwnsFullscreen=true; }
	}) : overlay.requestFullscreen();
	void fullscreen.catch(()=>status('全画面化できないため、ウィンドウ内で発表します'));
	void action(async()=>{
		try {
			await fullscreen.catch(()=>{});
			await flush();
			slidesInfo=await api.PrepareSlides();const deck=slidesInfo.deck;
			presentationIndex=first?0:Math.max(0,deck.slides.findIndex(s=>s.file===current));
			const token=++presentationToken;
			const slides=[];
			for(const slide of deck.slides) slides.push({...slide,html:await slideHTML(slide)});
			if(overlay.hidden||token!==presentationToken)return;
			updatePresentationPosition();
			sendFrame(presentationFrame,{type:'mdz-slide-render',token,slides,theme:deck.theme,aspect:deck.aspect,marginColor:deck.marginColor||'#ffffff',...frameTypography(deck),index:presentationIndex,presentation:true});
			presentationFrame.focus();
		} catch(error) {closePresentation();throw error;}
	});
}
function updatePresentationPosition(): void {const overlay=element('presentation');overlay.dataset.index=String(presentationIndex);overlay.setAttribute('aria-label',`${slidesInfo?.deck.title}：${presentationIndex+1} / ${slidesInfo?.deck.slides.length || 0}（Escで終了）`);}
element('slides-present').onclick=()=>openPresentation(false);element('slides-start').onclick=()=>openPresentation(true);
document.addEventListener('fullscreenchange',()=>{if(!document.fullscreenElement && !element('presentation').hidden)closePresentation();});
window.addEventListener('keydown',event=>{
	if(element('presentation').hidden)return;
	if(event.key==='Escape'){event.preventDefault();closePresentation();}
	if(event.ctrlKey || event.metaKey || event.altKey)return;
	const command=event.key==='Enter'?(event.shiftKey?'prev':'next'):['ArrowRight','ArrowDown','PageDown',' '].includes(event.key)?'next':['ArrowLeft','ArrowUp','PageUp'].includes(event.key)?'prev':event.key.toLowerCase()==='o'?'overview':event.key==='Home'?'first':event.key==='End'?'last':'';
	if(command){event.preventDefault();presentationFrame.contentWindow?.postMessage({type:'mdz-slide-command',command},location.origin);}
});
window.addEventListener('message',event=>{
	const frame=event.source===slideFrame.contentWindow?slideFrame:event.source===presentationFrame.contentWindow?presentationFrame:[element<HTMLIFrameElement>('presenter-previous'),element<HTMLIFrameElement>('presenter-current'),element<HTMLIFrameElement>('presenter-next')].find(f=>event.source===f.contentWindow);
	if(!frame||event.origin!==location.origin)return;
	const data=event.data;
	if(data?.type==='mdz-slide-ready'){const payload=framePayloads.get(frame);if(payload)frame.contentWindow?.postMessage(payload,location.origin);return;}
	if(data?.token!==framePayloads.get(frame)?.token)return;
	if(data.type==='mdz-slide-rendered') frame.dataset.rendered=String(data.token);
	if(data.type==='mdz-slide-error'){status(data.message,true);return;}
	if(data.type==='mdz-slide-overflow' && frame===slideFrame)element('slide-overflow').hidden=!data.ids.includes(selectedSlide()?.id);
	if(data.type==='mdz-slide-exit')closePresentation();
	if(data.type==='mdz-slide-index' && frame===presentationFrame){presentationIndex=data.index;updatePresentationPosition();}
	if(data.type==='mdz-slide-navigate' && frame===slideFrame && !editing && (data.offset===-1 || data.offset===1))navigateSlide(data.offset);
	if(data.type==='mdz-slide-save')void save(data.saveAs===true);
	if(data.type==='mdz-slide-link') {
		if(/^https?:\/\//i.test(data.href)){if(confirm('外部ブラウザーで開きますか？\n'+data.href))window.runtime.BrowserOpenURL(data.href);return;}
		const target=resolveLink(data.href,data.file);
		if(target && slidesInfo?.deck.slides.some(s=>s.file===target.path)) {
			if(!element('presentation').hidden){presentationIndex=slidesInfo.deck.slides.findIndex(s=>s.file===target.path);presentationFrame.contentWindow?.postMessage({type:'mdz-slide-command',command:'goto',index:presentationIndex},location.origin);return;}
			void action(async()=>{await flush();await selectPage(target.path);});
		} else status('このリンクのページはスライド一覧にありません');
	}
});

let thumbnailResizeTimer=0;
new ResizeObserver(()=>{
	window.clearTimeout(thumbnailResizeTimer);
	thumbnailResizeTimer=window.setTimeout(()=>{
		if(state?.mode!=='slides'||!slidesInfo)return;
		const nav=element('pages'), scrollTop=element('sidebar').scrollTop;slideListKey='';renderSlideList(nav);element('sidebar').scrollTop=scrollTop;
	},150);
}).observe(element('sidebar'));

function showSlideMenu(x:number,y:number): void {
	if(state?.mode!=='slides')return;
	const menu=element('slide-context-menu');menu.replaceChildren();
	const add=(label:string,id:string,editable=false)=>{
		if(editable&&!editing)return;
		const item=document.createElement('button');item.textContent=label;item.setAttribute('role','menuitem');
		item.onclick=()=>{menu.hidden=true;element<HTMLButtonElement>(id).click();};menu.append(item);
	};
	add('資料・ページ設定','slides-settings');
	add('通常MDZへ変換','slides-to-document',true);
	for(const [op,label] of [['add','スライドを追加'],['duplicate','複製'],['up','上へ移動'],['down','下へ移動'],['remove','削除'],['undo','構成を元に戻す'],['redo','構成をやり直す']]) {
		if(!editing)break;
		const item=document.createElement('button');item.textContent=label;item.setAttribute('role','menuitem');
		item.onclick=()=>{menu.hidden=true;document.querySelector<HTMLButtonElement>(`[data-slide-op="${op}"]`)!.click();};menu.append(item);
	}
	add('Markdownを取り込む','slides-import',true);add('画像追加','image',true);add('作業データの復旧','recovery');
	menu.hidden=false;menu.style.left=`${Math.min(x,innerWidth-menu.offsetWidth-8)}px`;menu.style.top=`${Math.min(y,innerHeight-menu.offsetHeight-8)}px`;
	menu.querySelector<HTMLButtonElement>('button')?.focus({preventScroll:true});
}
element('sidebar').addEventListener('contextmenu',event=>{if(state?.mode==='slides'){event.preventDefault();showSlideMenu(event.clientX,event.clientY);}});
document.addEventListener('pointerdown',event=>{if(!element('slide-context-menu').contains(event.target as Node))element('slide-context-menu').hidden=true;});
window.addEventListener('keydown',event=>{const menu=element('slide-context-menu');if(menu.hidden)return;if(event.key==='Escape'){menu.hidden=true;event.preventDefault();}if(['ArrowDown','ArrowUp'].includes(event.key)){event.preventDefault();const buttons=[...menu.querySelectorAll<HTMLButtonElement>('button')];const i=buttons.indexOf(document.activeElement as HTMLButtonElement);buttons[(i+(event.key==='ArrowDown'?1:-1)+buttons.length)%buttons.length].focus();}});

let dependencyCheck=0, dependencyTimer=0, detectedInitPath='';
async function checkDependencies():Promise<void>{
	const request=++dependencyCheck;
	for(const id of ['nvim-status','init-status','mdbook-status'])element(id).textContent='確認中…';
	try{
		const initInput=element<HTMLInputElement>('init-path');
		const results=await api.CheckDependencies(element<HTMLInputElement>('nvim-path').value,initInput.value,element<HTMLInputElement>('mdbook-path').value);
		if(request!==dependencyCheck)return;
		for(const result of results){
			const output=element(result.name==='Neovim'?'nvim-status':result.name==='init.lua'?'init-status':'mdbook-status');
			output.textContent=result.message;output.dataset.found=String(result.found);
			if(result.name==='init.lua' && result.path && (!initInput.value.trim() || initInput.value===detectedInitPath)){
				initInput.value=result.path;
				detectedInitPath=result.path;
			}
		}
	}
	catch(error){if(request===dependencyCheck)for(const id of ['nvim-status','init-status','mdbook-status'])element(id).textContent=String(error);}
}
element('check-dependencies').onclick=()=>void checkDependencies();
for(const id of ['nvim-path','init-path','mdbook-path'])element(id).addEventListener('input',()=>{window.clearTimeout(dependencyTimer);dependencyTimer=window.setTimeout(()=>void checkDependencies(),300);});

let dualDeck: SlideDeck | undefined;
let dualSlides: Array<Slide & {html:string}> = [];
let dualID='', dualIndex=-1, dualGeneration=0, dualTimerStart=0, dualElapsed=0, dualPaused=false;
const presenter=element('presenter');
async function dualCommand(command:string,index=0):Promise<void>{
	try{await api.PresentationCommand(command,index)}catch(error){element('presenter-error').textContent=String(error)}
}
function updatePresenter(index:number):void{
	if(!dualDeck||index===dualIndex)return;
	dualIndex=index;
	element<HTMLSelectElement>('presenter-page').value=String(index);
	element('presenter-notes').textContent=dualDeck.slides[index]?.notes||'このページにノートはありません。';
	for(const [id,i] of [['presenter-previous',Math.max(0,index-1)],['presenter-current',index],['presenter-next',Math.min(index+1,dualSlides.length-1)]] as const) {
		const frame=element<HTMLIFrameElement>(id);
		sendFrame(frame,{type:'mdz-slide-render',token:++presentationToken,slides:[dualSlides[i]],theme:dualDeck.theme,aspect:dualDeck.aspect,marginColor:dualDeck.marginColor||'#ffffff',...frameTypography(dualDeck),index:0,presentation:false});
	}
	element('presenter-previous').hidden=index===0;element('presenter-first').hidden=index!==0;
	element('presenter-next').hidden=index===dualSlides.length-1;element('presenter-last').hidden=index!==dualSlides.length-1;
}
async function stopDual():Promise<void>{
	++dualGeneration;presenter.hidden=true;dualDeck=undefined;dualID='';
	await api.StopPresentation();
	element('slides-dual').focus({preventScroll:true});
}
async function pollDual(generation:number):Promise<void>{
	if(generation!==dualGeneration||presenter.hidden)return;
	try{
		const state=await api.PresentationState();
		if(generation!==dualGeneration)return;
		if(state.closed||state.id!==dualID){await stopDual();status('投影用ウィンドウが閉じられました');return}
		updatePresenter(state.index);
		element('presenter-connection').textContent=state.ready?'投影用ウィンドウに接続済み':'投影用ウィンドウを開いています…';
		element('presenter-fullscreen').textContent=state.fullscreen?'投影の全画面を解除 (F)':'投影を全画面にする (F)';
	}catch(error){element('presenter-error').textContent=String(error)}
	window.setTimeout(()=>void pollDual(generation),200);
}
element('slides-dual').onclick=()=>void action(async()=>{
	await flush();slidesInfo=await api.PrepareSlides();dualDeck=structuredClone(slidesInfo.deck);dualSlides=[];
	for(const slide of dualDeck.slides)dualSlides.push({...slide,fontSize:dualDeck.bodyFontSize || slide.fontSize,html:await slideHTML(slide)});
	const index=Math.max(0,dualDeck.slides.findIndex(s=>s.file===current));
	const state=await api.StartPresentation(dualSlides,index);dualID=state.id;dualIndex=-1;
	const chooser=element<HTMLSelectElement>('presenter-page');chooser.replaceChildren();
	dualDeck.slides.forEach((slide,i)=>{const option=document.createElement('option');option.value=String(i);option.textContent=`${i+1} / ${dualSlides.length} — ${slide.title}`;chooser.append(option)});
	dualElapsed=0;dualTimerStart=performance.now();dualPaused=false;element('presenter-pause').textContent='一時停止';element('presenter-error').textContent='';
	presenter.hidden=false;presenter.focus();updatePresenter(index);void pollDual(++dualGeneration);
});
element('presenter-prev').onclick=()=>void dualCommand('prev');element('presenter-forward').onclick=()=>void dualCommand('next');
element('presenter-fullscreen').onclick=()=>void dualCommand('fullscreen');element('presenter-stop').onclick=()=>void stopDual().catch(error=>status(String(error),true));
element('presenter-page').onchange=()=>void dualCommand('goto',Number(element<HTMLSelectElement>('presenter-page').value));
element('presenter-pause').onclick=()=>{if(!dualPaused)dualElapsed+=performance.now()-dualTimerStart;else dualTimerStart=performance.now();dualPaused=!dualPaused;element('presenter-pause').textContent=dualPaused?'再開':'一時停止';};
element('presenter-reset').onclick=()=>{dualElapsed=0;dualTimerStart=performance.now();};
window.setInterval(()=>{
	if(presenter.hidden)return;
	element('presenter-clock').textContent=new Date().toLocaleTimeString('ja-JP',{hour12:false});
	const total=Math.floor((dualElapsed+(dualPaused?0:performance.now()-dualTimerStart))/1000), hours=Math.floor(total/3600);
	element('presenter-timer').textContent=(hours?String(hours).padStart(2,'0')+':':'')+String(Math.floor(total/60)%60).padStart(2,'0')+':'+String(total%60).padStart(2,'0');
},250);
window.addEventListener('keydown',event=>{
	if(presenter.hidden||event.ctrlKey||event.metaKey||event.altKey)return;
	if(event.key==='Escape'){event.preventDefault();void stopDual().catch(error=>status(String(error),true));return;}
	if((event.target as HTMLElement).matches('select,input,textarea')&&!['f','Escape'].includes(event.key))return;
	const command=event.key.toLowerCase()==='f'?'fullscreen':event.key==='Home'?'first':event.key==='End'?'last':event.key==='Enter'?(event.shiftKey?'prev':'next'):['ArrowRight','ArrowDown','PageDown',' '].includes(event.key)?'next':['ArrowLeft','ArrowUp','PageUp'].includes(event.key)?'prev':'';
	if(command){event.preventDefault();void dualCommand(command)}
});

function slideGap(event:DragEvent):{card:HTMLElement;after:boolean}|undefined{
	const cards=[...element('pages').querySelectorAll<HTMLElement>('.slide-card')];if(!cards.length)return;
	for(const card of cards){const r=card.getBoundingClientRect();if(event.clientY<r.top+r.height/2)return{card,after:false};}
	return{card:cards[cards.length-1],after:true};
}
element('pages').addEventListener('dragover',event=>{
	if(state?.mode!=='slides'||!editing||(event.target as Element).closest('.slide-card'))return;
	if(!event.dataTransfer?.types.includes('application/x-mdz-slide'))return;
	event.preventDefault();const gap=slideGap(event);if(!gap)return;
	element('pages').querySelectorAll('.slide-insert-before,.slide-insert-after').forEach(n=>n.classList.remove('slide-insert-before','slide-insert-after'));
	gap.card.classList.add(gap.after?'slide-insert-after':'slide-insert-before');
});
element('pages').addEventListener('drop',event=>{
	if(state?.mode!=='slides'||!editing)return;
	const id=event.dataTransfer?.getData('application/x-mdz-slide'),gap=slideGap(event);if(!id||!gap)return;
	event.preventDefault();event.stopPropagation();
	element('pages').querySelectorAll('.slide-insert-before,.slide-insert-after').forEach(n=>n.classList.remove('slide-insert-before','slide-insert-after'));
	void action(()=>mutateSlide(gap.after?'move-after':'move-before',gap.card.dataset.slide!,id));
});

// フレームの外側でも右クリックで発表を終了します。
element('presentation').addEventListener('contextmenu',event=>{
	if(element('presentation').hidden)return;
	event.preventDefault();closePresentation();
});
presenter.addEventListener('contextmenu',event=>{
	if(presenter.hidden)return;
	event.preventDefault();void stopDual().catch(error=>status(String(error),true));
});
