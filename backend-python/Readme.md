# backend-python

FastAPI と Uvicorn で共通ログ検索APIを提供し、HTTP経由で Trino を検索する Python バックエンドです。コンテナ内では `5000` ポートを使用します。

## ファイル構成

```text
backend-python/
├── Dockerfile          # Python実行環境とUvicorn起動設定
├── Readme.md           # このファイル
├── app.py              # FastAPIルート、入力正規化、JSONレスポンス
├── backend_factory.py  # ログ検索バックエンドの生成
├── requirements.txt    # FastAPI、Uvicorn、requests、pytest
└── trino_backend.py    # Trinoクライアント、SQL生成、結果整形
```

## API

- `GET /`: サービス情報
- `GET /health`: Trino疎通確認
- `GET /api/options`: ログ種別一覧
- `GET|POST /api/logs`: ログ検索

## ビルド

```bash
docker compose build backend-python
docker compose up -d backend-python
```

接続設定はルートの `.env.example` に記載された `TRINO_*` 環境変数を使用します。
