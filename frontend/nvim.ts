interface Cell { text: string; hl: number }
interface Highlight { foreground?: number; background?: number; special?: number; bold?: boolean; italic?: boolean; reverse?: boolean; underline?: boolean; undercurl?: boolean; strikethrough?: boolean }
export interface Transport {
	input(keys: string): void;
	paste(text: string): void;
	resize(cols: number, rows: number): void;
	mouse(button: string, action: string, modifier: string, row: number, col: number): void;
}

// NativeView はNeovimのext_linegridを描画し、IMEを含む入力を本体へ渡します。
export class NativeView {
	private grid: Cell[][] = [];
	private highlights = new Map<number, Highlight>();
	private cols = 80; private rows = 24;
	private fg = '#dce7df'; private bg = '#1d2520';
	private cursor = { row: 0, col: 0 };
	private mode = 'normal';
	private width = 9; private height = 23; private size = 15;
	private font = "Consolas, 'Yu Gothic', monospace";
	private active = false; private composing = false; private frame = 0;
	private dragging = false;
	private context: CanvasRenderingContext2D;
	constructor(private host: HTMLElement, private canvas: HTMLCanvasElement, private input: HTMLTextAreaElement, private transport: Transport) {
		this.context = canvas.getContext('2d')!;
		new ResizeObserver(() => this.resize()).observe(host);
		canvas.addEventListener('mousedown', event => {
			if (event.button !== 0) return;
			event.preventDefault(); this.focus(); this.dragging = true; this.mouse(event, 'left', 'press');
		});
		canvas.addEventListener('mousemove', event => { if (this.dragging) this.mouse(event, 'left', 'drag'); });
		window.addEventListener('mouseup', event => { if (this.dragging) { this.dragging = false; this.mouse(event, 'left', 'release'); } });
		canvas.addEventListener('wheel', event => {
			event.preventDefault(); this.mouse(event, 'wheel', event.deltaY > 0 ? 'down' : 'up');
		}, { passive: false });
		input.addEventListener('compositionstart', () => { this.composing = true; input.classList.add('composing'); });
		input.addEventListener('compositionend', event => {
			this.composing = false; input.classList.remove('composing');
			if (event.data) this.literal(event.data);
			input.value = '';
		});
		input.addEventListener('input', event => {
			if (this.composing || (event as InputEvent).isComposing) return;
			if ((event as InputEvent).inputType === 'insertFromComposition') { input.value = ''; return; }
			if (input.value) { this.literal(input.value); input.value = ''; }
		});
		input.addEventListener('keydown', event => this.key(event));
		input.addEventListener('focus', () => this.draw());
		input.addEventListener('blur', () => this.draw());
	}
	setActive(active: boolean): void { this.active = active; this.host.hidden = !active; if (active) this.resize(); }
	configure(family: string, size: number): void { this.font = family; this.size = size; this.resize(); }
	focus(): void { if (this.active) this.input.focus({ preventScroll: true }); }
	reset(): void { this.grid = []; this.highlights.clear(); this.cursor = { row: 0, col: 0 }; }
	resize(): void {
		if (!this.active || !this.host.clientWidth || !this.host.clientHeight) return;
		this.context.font = `${this.size}px ${this.font}`;
		this.width = this.context.measureText('M').width; this.height = Math.ceil(this.size * 1.5);
		const cols = Math.max(10, Math.floor(this.host.clientWidth / this.width));
		const rows = Math.max(3, Math.floor(this.host.clientHeight / this.height));
		const ratio = window.devicePixelRatio || 1;
		this.canvas.width = Math.floor(this.host.clientWidth * ratio); this.canvas.height = Math.floor(this.host.clientHeight * ratio);
		this.canvas.style.width = `${this.host.clientWidth}px`; this.canvas.style.height = `${this.host.clientHeight}px`;
		this.context.setTransform(ratio, 0, 0, ratio, 0, 0);
		this.transport.resize(cols, rows); this.draw();
	}
	redraw(events: unknown): void {
		if (!Array.isArray(events)) return;
		for (const event of events) {
			if (!Array.isArray(event)) continue;
			const name = event[0];
			for (const values of event.slice(1)) {
				if (!Array.isArray(values)) continue;
				const v = values;
				switch (name) {
				case 'grid_resize':
					if (v[0] !== 1) break;
					this.cols = v[1]; this.rows = v[2]; this.grid = Array.from({ length: this.rows }, (_, row) => Array.from({ length: this.cols }, (_, col) => this.grid[row]?.[col] || { text: ' ', hl: 0 })); break;
				case 'grid_clear': if (v[0] === 1) this.grid = Array.from({ length: this.rows }, () => Array.from({ length: this.cols }, () => ({ text: ' ', hl: 0 }))); break;
				case 'default_colors_set': this.fg = this.color(v[0], '#dce7df'); this.bg = this.color(v[1], '#1d2520'); break;
				case 'hl_attr_define': this.highlights.set(v[0], v[1]); break;
				case 'grid_line': {
					if (v[0] !== 1) break;
					let col = v[2], hl = 0;
					for (const cell of v[3]) {
						if (cell.length > 1) hl = cell[1];
						for (let n = 0; n < (cell[2] || 1); n++) { if (this.grid[v[1]] && col < this.cols) this.grid[v[1]][col] = { text: cell[0], hl }; col++; }
					} break;
				}
				case 'grid_scroll': {
					if (v[0] !== 1) break;
					const [, top, bottom, left, right, rows, cols] = v;
					const old = this.grid.map(row => row.slice());
					for (let y = top; y < bottom; y++) for (let x = left; x < right; x++) {
						this.grid[y][x] = y + rows >= top && y + rows < bottom && x + cols >= left && x + cols < right ? old[y + rows][x + cols] : { text: ' ', hl: 0 };
					} break;
				}
				case 'grid_cursor_goto': if (v[0] === 1) this.cursor = { row: v[1], col: v[2] }; break;
				case 'mode_change': this.mode = v[0]; this.input.setAttribute('aria-label', `Neovim ${this.mode}`); break;
				}
			}
		}
		cancelAnimationFrame(this.frame); this.frame = requestAnimationFrame(() => this.draw());
	}
	private color(value: number | undefined, fallback: string): string { return value === undefined || value < 0 ? fallback : '#' + value.toString(16).padStart(6, '0'); }
	private draw(): void {
		const ctx = this.context;
		ctx.fillStyle = this.bg; ctx.fillRect(0, 0, this.host.clientWidth, this.host.clientHeight);
		// 全角文字の続きセルが文字を消さないよう、背景と文字を別々に描きます。
		for (let pass = 0; pass < 2; pass++) for (let row = 0; row < this.grid.length; row++) for (let col = 0; col < this.grid[row].length; col++) {
			const cell = this.grid[row][col], hl = this.highlights.get(cell.hl) || {};
			let fg = this.color(hl.foreground, this.fg), bg = this.color(hl.background, this.bg);
			if (hl.reverse) [fg, bg] = [bg, fg];
			const x = col * this.width, y = row * this.height;
			if (pass === 0) { ctx.fillStyle = bg; ctx.fillRect(x, y, this.width + .5, this.height); continue; }
			if (!cell.text) continue;
			ctx.font = `${hl.italic ? 'italic ' : ''}${hl.bold ? 'bold ' : ''}${this.size}px ${this.font}`;
			ctx.fillStyle = fg; ctx.textBaseline = 'alphabetic'; ctx.fillText(cell.text, x, y + this.size + 2);
			if (hl.underline || hl.undercurl) ctx.fillRect(x, y + this.height - 3, this.width, 1);
			if (hl.strikethrough) ctx.fillRect(x, y + this.height / 2, this.width, 1);
		}
		const x = this.cursor.col * this.width, y = this.cursor.row * this.height;
		ctx.fillStyle = this.fg; ctx.globalAlpha = .45;
		ctx.fillRect(x, y, this.mode.startsWith('insert') ? 2 : this.width, this.height); ctx.globalAlpha = 1;
		this.input.style.left = `${Math.min(x, Math.max(0, this.host.clientWidth - 20))}px`;
		this.input.style.top = `${Math.min(y, Math.max(0, this.host.clientHeight - this.height))}px`;
		this.input.style.font = `${this.size}px ${this.font}`;
	}
	private literal(text: string): void { this.transport.input(text.replaceAll('<', '<LT>')); }
	private key(event: KeyboardEvent): void {
		if (event.isComposing || this.composing || event.keyCode === 229) return;
		// アプリ操作とクリップボード処理は外側へ渡します。
		if ((event.ctrlKey || event.metaKey) && ['s', 'v'].includes(event.key.toLowerCase())) return;
		const names: Record<string, string> = { Escape: 'Esc', Enter: 'CR', Backspace: 'BS', Delete: 'Del', Tab: 'Tab', ArrowUp: 'Up', ArrowDown: 'Down', ArrowLeft: 'Left', ArrowRight: 'Right', Home: 'Home', End: 'End', PageUp: 'PageUp', PageDown: 'PageDown', Insert: 'Insert', ' ': 'Space' };
		const special = names[event.key] || (/^F\d+$/.test(event.key) ? event.key : '');
		if (special || ((event.ctrlKey || event.altKey || event.metaKey) && !event.getModifierState('AltGraph') && event.key.length === 1)) {
			event.preventDefault();
			const mods = `${event.ctrlKey ? 'C-' : ''}${event.altKey ? 'A-' : ''}${event.metaKey ? 'D-' : ''}${event.shiftKey ? 'S-' : ''}`;
			this.transport.input(`<${mods}${special || event.key.toLowerCase()}>`);
		}
	}
	private mouse(event: MouseEvent, button: string, action: string): void {
		const rect = this.canvas.getBoundingClientRect();
		const row = Math.min(this.rows - 1, Math.max(0, Math.floor((event.clientY - rect.top) / this.height)));
		const col = Math.min(this.cols - 1, Math.max(0, Math.floor((event.clientX - rect.left) / this.width)));
		this.transport.mouse(button, action, `${event.ctrlKey ? 'C-' : ''}${event.shiftKey ? 'S-' : ''}${event.altKey ? 'A-' : ''}`, row, col);
	}
}
