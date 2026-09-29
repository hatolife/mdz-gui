export interface TextState { text: string; start: number; end: number }
interface Edit { before: TextState; after: TextState; kind: string; at: number }

// History は保存やページ切替で消えない、上限付きの編集履歴です。
export class History {
	private past: Edit[] = [];
	private future: Edit[] = [];
	private separated = true;
	constructor(private limit: number) {}
	setLimit(limit: number): void { this.limit = limit; this.trim(); }
	canUndo(): boolean { return this.past.length > 0; }
	canRedo(): boolean { return this.future.length > 0; }
	separate(): void { this.separated = true; }
	record(before: TextState, after: TextState, kind: string): void {
		if (before.text === after.text) return;
		const last = this.past.at(-1);
		const now = Date.now();
		const typing = ['insertText', 'deleteContentBackward', 'deleteContentForward'].includes(kind);
		if (!this.separated && typing && last?.kind === kind && now - last.at < 700 && last.after.text === before.text && last.after.start === before.start && last.after.end === before.end) {
			last.after = after; last.at = now;
		} else this.past.push({ before, after, kind, at: now });
		this.separated = !typing; this.future = []; this.trim();
	}
	undo(): TextState | undefined { const edit = this.past.pop(); if (!edit) return; this.future.push(edit); this.separate(); return edit.before; }
	redo(): TextState | undefined { const edit = this.future.pop(); if (!edit) return; this.past.push(edit); this.separate(); return edit.after; }
	private trim(): void {
		while (this.past.length > this.limit) this.past.shift();
		while (this.future.length > this.limit) this.future.shift();
		// 内蔵エディターの巨大な全文履歴がメモリを使い切らないようにします。
		let bytes = this.past.reduce((n, e) => n + 2 * (e.before.text.length + e.after.text.length), 0);
		while (bytes > 64 * 1024 * 1024 && this.past.length > 1) {
			const e = this.past.shift()!; bytes -= 2 * (e.before.text.length + e.after.text.length);
		}
	}
}
