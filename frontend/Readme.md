# frontend

Trino ログ検索画面の静的ファイルを配信し、各言語バックエンドへのリバースプロキシを行う Nginx コンテナです。Compose で外部公開されるのはこのコンテナの `8081` ポートだけです。

## ファイル構成

```text
frontend/
├── Dockerfile          # アセット生成用ステージと Nginx 実行イメージ
├── Readme.md           # このファイル
├── build-assets.sh     # CSS/JSへ内容ハッシュを付けて dist を生成
├── index.html          # SPA のHTMLシェル
├── nginx.conf          # 静的配信、API/healthルーティング、ログ設定
├── proxy-common.conf   # バックエンド転送時の共通HTTPヘッダー
├── search.js           # 画面描画、検索、ヘルス監視、CSV出力
└── styles.css          # 全画面共通スタイルとレスポンシブ表示
```

## ルーティング

- `/`、`/search`、`/health`: `index.html` を返すSPAルート
- `/api/{backend}/...`: 選択したバックエンドの `/api/...` へ転送
- `/health/{backend}`: 各バックエンドの `/health` へ転送

`backend` は `flask`、`go`、`java`、`php`、`ruby`、`elixir` のいずれかです。

## ビルド

```bash
docker compose build frontend
docker compose up -d frontend
```

`build-assets.sh` は `styles.css` と `search.js` の内容ハッシュをファイル名へ付与し、生成した名前に `index.html` を書き換えます。
