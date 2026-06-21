# backend-go

Go 標準ライブラリの HTTP サーバーで共通ログ検索APIを提供するバックエンドです。Trino REST APIへの接続、SQL生成、レスポンス整形までを行い、コンテナ内では `5000` ポートを使用します。

## ファイル構成

```text
backend-go/
├── Dockerfile  # マルチステージビルドと実行イメージ
├── Readme.md   # このファイル
├── go.mod      # Goモジュール定義
└── main.go     # HTTP、Trino通信、SQL生成、結果整形
```

`main.go` は共通APIを他言語実装と比較しやすいよう、現在は単一ファイルにまとめています。分割する場合は `http`、`trino`、`query` の3責務が境界候補です。

## API

- `GET /`
- `GET /health`
- `GET /api/options`
- `GET|POST /api/logs`

## ビルド

```bash
docker compose build backend-go
docker compose up -d backend-go
```

接続設定は `TRINO_*` 環境変数を使用します。
