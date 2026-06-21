# backend-go

Go 標準ライブラリの HTTP サーバーで共通ログ検索APIを提供するバックエンドです。Trino REST APIへの接続、SQL生成、レスポンス整形までを行い、コンテナ内では `5000` ポートを使用します。

## ファイル構成

```text
backend-go/
├── Dockerfile      # マルチステージビルドと実行イメージ
├── Readme.md       # このファイル
├── config.go       # 環境変数と共通設定
├── go.mod          # Goモジュール定義
├── http.go         # HTTPルート、入力正規化、JSONレスポンス
├── main.go         # サーバー起動
├── models.go       # APIのデータ型
├── query.go        # SQL生成と検索結果整形
├── time_format.go  # 検索時刻と表示時刻の変換
└── trino.go        # Trino REST APIクライアント
```

HTTP、Trino通信、SQL生成を独立したファイルに分け、設定・モデル・時刻変換もそれぞれの責務へ切り出しています。

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
