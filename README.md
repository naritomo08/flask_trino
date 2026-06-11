# flask_trino

Trino から参照できる Iceberg の `syslog_events` / `authlog_events` テーブルを Java で検索する Web アプリです。

設定は以下のリポジトリの Trino / Iceberg 利用版に合わせています。

https://github.com/naritomo08/elixir_trino

Trino は以下の記事の構成で作成済みのものを利用する想定です。

https://qiita.com/naritomo08/items/f228fe97d152c16a95d8

検索対象日は JST の当日固定です。画面では開始時刻と終了時刻だけを指定し、条件に一致したログのうち最新 50 件を表示します。

## 起動

```bash
docker compose up --build
```

ブラウザで http://localhost:5004 を開きます。

Java アプリだけを Docker で起動します。Trino / Iceberg / 収集基盤はこの Compose には含めません。
画面検索は POST 後に GET へリダイレクトするため、リロードしてもフォーム再送信は発生しません。

## ローカル実行

Java 21 と Maven が入っている環境では以下でも起動できます。

```bash
mvn test
mvn package
java -jar target/flask-trino-1.0.0.jar
```

ブラウザで http://localhost:5000 を開きます。

## 前提テーブル

デフォルトでは以下の Trino テーブルを検索します。

- `iceberg.logs.syslog_events`
- `iceberg.logs.authlog_events`

検索と表示に使うカラムは以下です。

- `ts`: ログ時刻
- `host`: ホスト名
- `program`: プログラム名
- `message`: メッセージ

カラム名やテーブル名が違う場合は環境変数で変更してください。
時刻カラムが文字列などでそのまま比較できない場合は、`TRINO_TIMESTAMP_EXPRESSION` に Trino の SQL 式を設定できます。

例:

```yaml
environment:
  TRINO_TIMESTAMP_COLUMN: ts
  TRINO_TIMESTAMP_EXPRESSION: CAST("ts" AS timestamp)
```

## API

ログ検索 (POST):

```bash
curl -X POST http://localhost:5004/api/logs \
  -H "Content-Type: application/json" \
  -d '{
    "time_from":"09:00",
    "time_to":"10:30",
    "log_type":"syslog",
    "host":"trino1",
    "program":"sshd",
    "message":"timeout"
  }'
```

ログ検索 (GET):

```bash
curl "http://localhost:5004/api/logs?time_from=09:00&time_to=10:30&log_type=syslog&message=timeout"
```

ヘルスチェック:

```bash
curl http://localhost:5004/health
```

## テスト

JUnit で主要処理を確認できます。
テストでは外部の Trino に実接続せず、Fake クライアントを使います。

実行方法:

```bash
mvn test
```

Docker で確認する場合:

```bash
docker compose build
```

Dockerfile のビルドステージで `mvn test package` を実行します。

確認している内容:

- JST の時刻表示変換
- JST 当日の時刻範囲を Trino SQL に変換する処理
- `syslog_events` / `authlog_events` を対象にした SQL 生成
- 検索結果の表示用フィールド作成

## 設定

`docker-compose.yml` の環境変数で接続先やテーブル名を変更できます。

- `TRINO_URL`: Trino coordinator の URL
- `TRINO_USER`: Trino に渡すユーザー名
- `TRINO_PASSWORD`: Basic 認証が必要な場合のパスワード
- `TRINO_CATALOG`: Trino catalog
- `TRINO_SCHEMA`: Trino schema
- `TRINO_SYSLOG_TABLE`: syslog 検索対象テーブル
- `TRINO_AUTHLOG_TABLE`: authlog 検索対象テーブル
- `TRINO_TIMESTAMP_COLUMN`: ログ時刻カラム
- `TRINO_TIMESTAMP_EXPRESSION`: ログ時刻の SQL 式。指定時は `TRINO_TIMESTAMP_COLUMN` より優先
- `TRINO_LIMIT`: 最大取得件数
- `PORT`: HTTP ポート

例:

```yaml
environment:
  TRINO_URL: http://trino1:8080
  TRINO_USER: log_search
  TRINO_CATALOG: iceberg
  TRINO_SCHEMA: logs
  TRINO_SYSLOG_TABLE: syslog_events
  TRINO_AUTHLOG_TABLE: authlog_events
  TRINO_TIMESTAMP_COLUMN: ts
extra_hosts:
  - "trino1:192.168.11.18"
```

## 他言語版

本サイトは Java 版です。
ブランチを切り替えれば PHP / Go / Ruby 版にもなります。

ブランチ名がそのままその言語版になります。

言語比較やパフォーマンス比較にもご利用ください。
