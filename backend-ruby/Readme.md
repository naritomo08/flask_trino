# backend-ruby

Sinatra と Rack/Puma で共通ログ検索APIを提供する Ruby バックエンドです。コンテナ内では `5000` ポートを使用します。

## ファイル構成

```text
backend-ruby/
├── Dockerfile  # Ruby実行環境、依存導入、Rack起動
├── Gemfile     # Sinatra、Puma、Rackとテスト依存
├── Readme.md   # このファイル
├── app.rb      # Sinatraルート、Trino通信、SQL生成、結果整形
└── config.ru   # Rackエントリーポイント
```

`app.rb` を分割する場合は `TrinoClient` と `LogSearchApp` 内の検索ロジックが境界候補です。現状は他言語版との対応を確認しやすい単一実装として維持しています。

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
