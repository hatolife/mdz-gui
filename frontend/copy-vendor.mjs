import { copyFile, mkdir } from 'node:fs/promises';
// 発表時に外部サイトへ接続せず、配布EXEの資源だけで描画します。
await mkdir('dist/vendor', { recursive: true });
for (const [source, target] of [['dist/reveal.esm.js', 'reveal.esm.js'], ['dist/reveal.css', 'reveal.css'], ['LICENSE', 'reveal-LICENSE.txt']]) {
	await copyFile(`node_modules/reveal.js/${source}`, `dist/vendor/${target}`);
}
