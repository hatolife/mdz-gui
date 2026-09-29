import { chromium } from 'playwright';
import JSZip from 'jszip';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as os from 'node:os';
import assert from 'node:assert/strict';

// 実バックエンドで画面の配置と操作列を検証します。
const url = fs.readFileSync(process.env.MDZ_BRIDGE_URL!, 'utf8');
const output = fs.mkdtempSync(path.join(os.tmpdir(), 'mdz-layout-'));
const rpc = async (name: string, ...args: unknown[]) => {
	const response = await fetch(url + '/rpc/' + name, { method: 'POST', body: JSON.stringify(args) });
	if (!response.ok) throw new Error(await response.text());
	return response.json();
};
const browser = await chromium.launch({ headless: true, executablePath: process.env.MDZ_CHROMIUM || undefined, args: ['--no-sandbox', '--disable-gpu'] });
const page = await browser.newPage({ viewport: { width: 1280, height: 860 } });
const errors: string[] = [];
page.on('pageerror', error => errors.push(error.message));
await page.addInitScript(() => {
	if (window.top !== window) return;
	const listeners = {};
	window.go = { main: { App: new Proxy({}, { get: (_, name) => async (...args) => {
		const response = await fetch('/rpc/' + String(name), { method: 'POST', body: JSON.stringify(args) });
		if (!response.ok) throw new Error(await response.text());
		return response.json();
	} }) } };
	window.windowActions = [];
	window.runtime = {
		OnFileDrop: () => {}, EventsOn: (name, callback) => { (listeners[name] ??= []).push(callback); }, BrowserOpenURL: () => {},
		WindowMinimise: () => window.windowActions.push('minimise'),
		WindowToggleMaximise: () => window.windowActions.push('maximise'),
		Quit: () => window.windowActions.push('quit'),
	};
	let pending = false;
	setInterval(async () => {
		if (pending) return;
		pending = true;
		try { for (const event of await (await fetch('/events')).json()) for (const callback of listeners[event.name] || []) callback(event.data); }
		finally { pending = false; }
	}, 50);
});
const ready = () => page.waitForFunction(() => !document.querySelector<HTMLButtonElement>('#new')!.disabled);
async function screenshot(name: string) {
	await page.screenshot({ path: path.join(output, name + '.png') });
}
async function verifyDraggable(selector: string, expected: 'drag' | 'no-drag') {
	assert.equal(await page.locator(selector).evaluate(element => getComputedStyle(element).getPropertyValue('--wails-draggable').trim()), expected);
}
async function verifyBounds() {
	assert.equal(await page.locator('#titlebar').count(), 1);
	assert(await page.locator('#titlebar').isVisible());
	assert((await page.locator('#app-version').textContent())?.trim().length);
	assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
	for (const id of ['new', 'open', 'settings', 'window-minimise', 'window-maximise', 'window-close', 'editing']) {
		const control = page.locator('#' + id);
		if (!await control.isVisible()) continue;
		assert(await control.evaluate(element => {
			const rect = element.getBoundingClientRect();
			const hit = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2);
			return rect.x >= 0 && rect.y >= 0 && rect.right <= innerWidth && rect.bottom <= innerHeight && (hit === element || element.contains(hit));
		}), id + ' must remain visible and clickable');
	}
}
try {
	await page.goto(url); await ready(); await verifyBounds();
	await verifyDraggable('#titlebar', 'drag');
	await verifyDraggable('#window-controls', 'no-drag');
	await verifyDraggable('#welcome', 'drag');
	await verifyDraggable('#welcome-open', 'no-drag');
	await screenshot('home');
	for (const id of ['window-minimise', 'window-maximise', 'window-close']) await page.locator('#' + id).click();
	assert.deepEqual(await page.evaluate(() => window.windowActions), ['minimise', 'maximise', 'quit']);
	await page.locator('#new').click(); await page.locator('[data-new-kind=mdbook]').click(); await ready();
	assert(await page.locator('#book-heading').isVisible());
	await verifyDraggable('#sidebar', 'drag');
	await verifyDraggable('#toc-edit', 'no-drag');
	await verifyDraggable('.toolbar', 'drag');
	await verifyDraggable('#editing', 'no-drag');
	await page.locator('#preview-kind').selectOption('markdown'); await ready();
	await page.frameLocator('#preview').locator('h1').waitFor();
	await screenshot('book');
	const resizer = await page.locator('#sidebar-resizer').boundingBox();
	const workspace = await page.locator('#workspace').boundingBox();
	await page.mouse.move(resizer!.x + 1, resizer!.y + 100); await page.mouse.down();
	await page.mouse.move(workspace!.x + 290, resizer!.y + 100); await page.mouse.up();
	assert(Math.abs((await page.locator('#sidebar').boundingBox())!.width - 290) < 2);
	await page.locator('#sidebar-toggle').click(); assert.equal(await page.locator('#sidebar').isVisible(), false);
	await page.locator('#sidebar-toggle').click(); assert.equal(await page.locator('#sidebar').isVisible(), true);
	await page.locator('#editing').click(); await ready();
	assert.equal(await page.locator('#editor').isVisible(), false);
	assert.equal(await page.locator('#book-sidebar-bottom').isVisible(), false);
	await page.locator('#editing').click(); await ready();
	for (const theme of ['light', 'dark']) {
		for (const accent of ['blue', 'green', 'purple']) {
			await page.locator('#settings').click();
			await page.locator('[name=theme]').selectOption(theme);
			await page.locator('[name=accent]').selectOption(accent);
			await page.locator('#settings-save').click(); await ready();
			await page.locator('#settings-dialog').waitFor({ state: 'hidden' });
			assert.equal(await page.locator('html').getAttribute('data-accent'), accent);
			await page.setViewportSize({ width: 800, height: 520 }); await verifyBounds();
			await screenshot(theme + '-' + accent);
		}
	}
	const cfg = await rpc('Settings'); await rpc('Configure', { ...cfg, theme: 'light', accent: 'blue' });
	await rpc('Save', false);
	// 同梱ヘルプの実データを読み込み、長い目次でも操作列が維持されることを確認します。
	const zip = new JSZip();
	const helpRoot = path.resolve('..', 'docs', 'help');
	for (const entry of fs.readdirSync(helpRoot, { recursive: true, withFileTypes: true })) {
		if (entry.isFile()) {
			const filename = path.join(entry.parentPath, entry.name);
			zip.file(path.relative(helpRoot, filename).split(path.sep).join('/'), fs.readFileSync(filename));
		}
	}
	const helpFile = path.join(output, 'help.mdz');
	fs.writeFileSync(helpFile, await zip.generateAsync({ type: 'nodebuffer' }));
	await rpc('Open', helpFile); await page.reload(); await ready();
	await page.locator('#preview-kind').selectOption('markdown'); await ready();
	await page.setViewportSize({ width: 1280, height: 860 }); await verifyBounds(); await screenshot('help');
	await rpc('Save', false); await rpc('NewDocument', 'slides'); await page.reload(); await ready();
	await page.frameLocator('#slide-preview').locator('section h1').waitFor();
	assert.equal(await page.locator('#slides-overview').count(), 0);
	for (const width of [1280, 800]) {
		await page.setViewportSize({ width, height: width === 800 ? 520 : 860 }); await verifyBounds();
		const frame = await page.locator('#panes').boundingBox();
		const dock = await page.locator('#slides-toolbar').boundingBox();
		assert(dock!.y >= frame!.y + frame!.height - 1);
		await screenshot('slides-' + width);
	}
	await rpc('Save', false);
	// ブリッジは呼び出し元で終了します。
	assert.equal(errors.length, 0, errors.join('\n'));
	console.log('PASS: titlebar/version, window actions, sidebar resize, themes, home/book/help/slides, 800px layout');
} finally {
	await browser.close();
	await fetch(url + '/test-stop');
	console.log('Screenshots: ' + output);
}
