# backend-java

JDK組み込みの `HttpServer` と Jackson で共通ログ検索APIを提供する Java バックエンドです。Mavenで実行可能JARを作成し、コンテナ内の `5000` ポートで起動します。

## ファイル構成

```text
backend-java/
├── Dockerfile
├── Readme.md
├── pom.xml
└── src/main/java/com/example/flasktrino/
    └── App.java
```

- `Dockerfile`: MavenビルドとJRE実行イメージ
- `pom.xml`: Java 21、Jackson、JUnit、Shade Pluginの設定
- `App.java`: HTTPルート、設定、Trino通信、SQL生成、結果整形

`App.java` を分割する場合は `HttpHandlers`、`TrinoClient`、`QueryBuilder`、`Models` が自然な境界です。現状は他言語実装との対応を追いやすい単一ファイル構成です。

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
