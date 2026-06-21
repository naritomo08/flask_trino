# backend-java

JDK組み込みの `HttpServer` と Jackson で共通ログ検索APIを提供する Java バックエンドです。Mavenで実行可能JARを作成し、コンテナ内の `5000` ポートで起動します。

## ファイル構成

```text
backend-java/
├── Dockerfile
├── Readme.md
├── pom.xml
└── src/main/java/com/example/flasktrino/
    ├── App.java
    └── TrinoClient.java
```

- `Dockerfile`: MavenビルドとJRE実行イメージ
- `pom.xml`: Java 21、Jackson、JUnit、Shade Pluginの設定
- `App.java`: HTTPルート、設定、SQL生成、結果整形
- `TrinoClient.java`: Trino REST API通信

独立性の高いTrino通信を別クラスへ分離しています。今後さらに分ける場合は `HttpHandlers`、`QueryBuilder`、`Models` が自然な境界です。

## API

- `GET /`
- `GET /health`
- `GET /api/options`
- `GET|POST /api/logs`

## ビルド

```bash
docker compose build backend-java
docker compose up -d backend-java
```

接続設定は `TRINO_*`、待受ポートは `PORT` 環境変数を使用します。
