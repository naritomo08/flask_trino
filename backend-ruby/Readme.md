# backend-ruby

Sinatra と Rack/Puma で共通ログ検索APIを提供する Ruby バックエンドです。コンテナ内では `5000` ポートを使用します。

## ファイル構成

```text
backend-ruby/
├── Dockerfile       # Ruby実行環境、依存導入、Rack起動
├── Gemfile          # Sinatra、Puma、Rackとテスト依存
├── Readme.md        # このファイル
├── app.rb           # SinatraルートとJSONレスポンス
├── config.ru        # Rackエントリーポイント
├── log_search.rb    # 入力正規化、SQL生成、結果整形
└── trino_client.rb  # Trino REST APIクライアント
```

SinatraのHTTP層、ログ検索ロジック、Trino通信を独立したファイルに分けています。

## API

- `GET /`
- `GET /health`
- `GET /api/options`
- `GET|POST /api/logs`

## ビルド

```bash
docker compose build backend-ruby
docker compose up -d backend-ruby
```

接続設定は `TRINO_*` 環境変数を使用します。
