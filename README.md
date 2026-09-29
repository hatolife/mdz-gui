# mdz-gui

名前は仮です。

Markdownを閲覧・編集するWindowsアプリです。
普通の`.md`ファイルをそのまま開いて編集できます。
また、Markdownと画像を1つのファイルにまとめる**MDZ**形式にも対応しています。
MDZの中身は、テキストファイルと画像をまとめたただのZIPです(公式仕様: [MDZip仕様](https://github.com/mdzip-project/mdzip-spec/blob/main/SPEC.md))。
MDZは中身の構成をある程度自由に決められるため、mdz-guiではスライド形式とmdBook形式を独自にサポートしています。

## このアプリできること

| 機能 | 対象 | 内容 |
|---|---|---|
| Markdown | `.md` | 普通のMarkdownエディターとして閲覧・編集 |
| 一般的なMDZ | `.mdz` | 画像付き・複数ページの文書を1ファイルで閲覧・編集 |
| スライドのMDZ | `.mdz` | 発表資料を作成・編集し、そのまま発表(2画面発表も可) |
| mdBookのMDZ | `.mdz` | 章立てのある本を作成・編集し、実mdBookでプレビュー |

編集には内蔵エディターのほか、**本物のNeovim**も使えます。

詳しい操作は、同梱の`mdz-gui-help.mdz`をmdz-guiで開いて読んでください。



## インストール

[GitHub Releases](https://github.com/hatolife/mdz-gui/releases)から`mdz-gui-windows-amd64-日時.zip`をダウンロードして展開し、`mdz-gui.exe`を起動します。インストール作業は不要です。

| 必要なもの | 必須か | 説明 |
|---|---|---|
| WebView2 Runtime | 必須 | Windows 11には通常入っています |
| Neovim | 任意 | 0.11.5で動作確認済み。Windows・WSLのどちらでも可 |
| mdBook | 任意 | 未導入ならwingetで自動インストール。0.5.4で動作確認済み |

## 使用方法

1. 左端の「新規」→「通常のMDZを作成」を押します。
2. 文章を書き、画像を`Ctrl+V`で貼り付けます。画像は文書内に保存され、相対リンクが挿入されます。
3. `Ctrl+S`で保存します。

## 扱える文書

- **単一Markdown**(`.md` / `.markdown`): 既存のMarkdownをそのまま編集します。`Ctrl+Shift+S`でMDZとして保存できます。
- **通常のMDZ**(`.mdz`): 画像付きの資料です。複数ページを持てて、スライドと相互に変換できます。
- **mdBook**(`.mdz`): 章立てのある本です。目次と`book.toml`を専用画面で編集し、実際のmdBookでプレビューします。
- **スライド**(`.mdz`): 発表資料です。reveal.jsを同梱しているので、オフラインで発表できます。2画面での発表にも対応しています。

mdBookとスライドはこのアプリ独自の仕様でしかないので、中身を見ないと通常のmdzと区別はつきません。
zip内にslides.jsonがあればスライド用mdz、book.tomlがあればmdBook用mdzです。

mdBookがインストールされていない時下記のwingetコマンドでインストールされます。
```sh
winget install --id Rustlang.mdBook --exact --source winget --accept-source-agreements --accept-package-agreements --disable-interactivity
```

## 主なショートカット

| キー | 動作 |
|---|---|
| `Ctrl+O` / `Ctrl+S` | 開く / 保存 |
| `Ctrl+Shift+S` | 名前を付けて保存 |
| `Ctrl+V` | 貼り付け(画像は文書内へ保存) |
| `Ctrl+Z` / `Ctrl+Y` | 元に戻す / やり直す |

## 保存とデータ

- 保存は一時ファイル経由で行うため、失敗しても元のファイルは壊れません。外部で変更されていた場合は上書きを中止します。
- 上書き前のファイルをバックアップします(既定10世代・合計512MiB)。
- 異常終了時は、次回「作業データの復旧」から再開できます。
- 作業データ・バックアップ・設定は`%LOCALAPPDATA%\mdz-gui\`に置きます。
- スライドの発表者ノートはMDZに保存されます。**資料を渡すとノートも読まれます。**

## 対応形式と制限

- [MDZip仕様](https://github.com/mdzip-project/mdzip-spec/blob/main/SPEC.md) 1.x(保存時は1.1.0)。スライドは独自の`slides`モードです。
- GFMの表・チェックリスト・取り消し線、画像はPNG/JPEG/GIF/WebP/SVGに対応します。
- 上限: 1ファイル32MiB、1文書128MiB、4096ファイル。
- プレビューでは外部URLの画像、生のHTML、スクリプトを読み込みません(mdBookを除く)。
- 未対応: WYSIWYG、Mermaid、数式、ページの削除・改名、未使用画像の整理、ファイル関連付け、スライドの書き出しとアニメーション。

## コマンドライン

```sh
mdz-gui.exe document.md    # mdファイルを開く
mdz-gui.exe document.mdz   # mdzファイルを開く
mdz-gui.exe --help         # 使い方を表示
mdz-gui.exe --version      # バージョンを表示
```

## 開発者向け

必要なもの: Go 1.26、Node.js 22以降

単にローカルビルドしたい場合。clone後に下記を実行します。
```sh
wails build
```

## 残作業

- mdをhtmlやpdfに変換する機能の実装。
- mdzをhtmlやpdfに変換する機能の実装。
    - スライドやmdBookはそれ相応のレイアウトや機能を維持して変換したい。
- 必要ならその他の機能がある独自形式モードを実装。
    - 現状思いついていない。そんなに求めてない。
    - 現状WordとPowerPointの簡易版になっているが、かといってExcelが欲しいわけじゃない。機能もりもりしない。


