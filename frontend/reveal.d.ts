declare module '*reveal.esm.js' {
	export default class Reveal {
		constructor(element: HTMLElement, options: Record<string, unknown>);
		initialize(): Promise<void>;
		destroy(): void;
		slide(index: number): void;
		next(): void;
		prev(): void;
		getIndices(): {h: number};
		toggleOverview(value?: boolean): void;
		on(event: string, callback: () => void): void;
	}
}
