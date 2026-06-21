# backend-elixir

Plug/Cowboy で共通ログ検索APIを提供する Elixir バックエンドです。Mix Releaseとしてビルドし、コンテナ内の `5000` ポートで起動します。

## ファイル構成

```text
backend-elixir/
├── Dockerfile
├── Readme.md
├── mix.exs
├── mix.lock
├── config/
│   └── runtime.exs
└── lib/elixir_elastic/
    ├── application.ex
    ├── query_builder.ex
    ├── router.ex
    ├── trino_client.ex
    └── trino_search.ex
```

- `runtime.exs`: `TRINO_*` と `PORT` の実行時設定
- `application.ex`: SupervisorとCowboyの起動
- `router.ex`: APIルート、入力正規化、JSONレスポンス
- `query_builder.ex`: SQL生成と検索時刻の正規化
- `trino_client.ex`: Trino REST API通信
- `trino_search.ex`: 検索実行と結果整形

HTTP層、Trino通信、SQL生成、検索結果整形をそれぞれ分離しています。

## API

- `GET /`
- `GET /health`
- `GET /api/options`
- `GET|POST /api/logs`

## ビルド

```bash
docker compose build backend-elixir
docker compose up -d backend-elixir
```

接続設定は `TRINO_*`、待受ポートは `PORT` 環境変数を使用します。
