# mdBook形式

mdz-guiが独自にサポートしているMDZの形式の1つです。[mdBook](https://rust-lang.github.io/mdBook/)のプロジェクト一式を、そのまま1つの`.mdz`にまとめたものです。

## mdBookとは

Markdownから、目次付きの本(HTML)を作るツールです。Rustの公式ドキュメントなどで使われています。

- `SUMMARY.md`に書いた目次が、本の章立てと読む順番になります。
- `book.toml`に、本のタイトル・著者・言語などを書きます。

## MDZの中身

MDZを展開すると、普通のmdBookプロジェクトと同じ構成になっています。展開したフォルダーで`mdbook build`を実行すれば、そのまま本を生成できます。

```text
book.mdz
├── manifest.json      # MDZipのmanifest
├── book.toml          # 本の設定
└── src/
    ├── SUMMARY.md     # 目次
    ├── introduction.md
    ├── chapter-1.md
    └── images/
        └── figure.png
```

本文の置き場所は`book.toml`の`[book] src`で変えられます。省略すると`src`です。

## mdz-guiでの判定

MDZの直下に`book.toml`があれば、mdBook形式として扱います。ただし、スライド形式(`manifest.json`の`mode`が`slides`)の場合は除きます。

MDZipの仕様には、mdBook形式の定義はありません。ほかのMDZipアプリでは、Markdownファイルが入った普通のMDZとして開かれます。

## mdz-guiでできること

- 実際の`mdbook serve`を使ったプレビュー
- 目次(`SUMMARY.md`)の編集
- タイトルなど、本の設定(`book.toml`)の編集
- 「新規」→「mdBookを作成」によるひな形からの作成

## mdBookがない場合

mdBookが見つからない場合は、mdBook形式の文書を開いたときに、wingetでインストールするかを確認します。

インストールしない場合は、通常のMDZとして表示します。この選択は設定に保存されます。あとから入れたくなったときは、「設定」の「mdBookをインストール」を押してください。
