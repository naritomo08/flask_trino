# backend-php

Slim 4 と Apache で共通ログ検索APIを提供する PHP バックエンドです。Apache の DocumentRoot は `public/`、待受ポートはコンテナ内の `5000` です。

## ファイル構成

```text
backend-php/
├── Dockerfile          # Composer依存解決とApache実行イメージ
├── Readme.md           # このファイル
├── composer.json       # 依存関係とautoload対象
├── composer.lock       # 依存バージョン固定
├── router.php          # PHP組み込みサーバー用ルーター
├── public/
│   └── index.php       # Webエントリーポイント
└── src/
    ├── config.php      # 環境変数と定数
    ├── http.php        # Slimルート、入力、JSONレスポンス
    └── trino.php       # Trino通信、SQL生成、結果整形
```

HTTP層、設定、Trino処理は `src/` 内で分離済みです。`router.php` はローカルで `php -S ... router.php` を使う場合の補助で、DockerのApache起動では使用しません。

## API

- `GET /`
- `GET /health`
- `GET /api/options`
- `GET|POST /api/logs`

## ビルド

```bash
docker compose build backend-php
docker compose up -d backend-php
```

接続設定は `TRINO_*` 環境変数を使用します。
