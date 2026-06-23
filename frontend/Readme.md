# frontend

Trino ログ検索画面の静的ファイルを配信し、各言語バックエンドへのリバースプロキシを行う Nginx コンテナです。Compose で外部公開されるのはこのコンテナの `8081` ポートだけです。

## ファイル構成

```text
frontend/
├── Dockerfile          # アセット生成用ステージと Nginx 実行イメージ
├── Readme.md           # このファイル
├── build-assets.sh     # CSS/JSへ内容ハッシュを付けて dist を生成
├── css/                # 共通・画面別・レスポンシブのスタイル
├── index.html          # SPA のHTMLシェル
├── js/                 # ES Modulesで分割した画面・通信・共通処理
├── nginx.conf          # 静的配信、API/healthルーティング、ログ設定
└── proxy-common.conf   # バックエンド転送時の共通HTTPヘッダー
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

`build-assets.sh` は各CSSとJSのエントリーポイントへ内容ハッシュを付与し、生成した名前に `index.html` を書き換えます。JSはブラウザ標準のES Modulesで読み込みます。
