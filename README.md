# flask_trino

Trino から参照できる Iceberg の `syslog_events` / `authlog_events` テーブルを検索するログ検索サイトです。

`frontback` 構成では、Nginx の静的フロントエンドから各言語の Trino バックエンドへ API プロキシします。参照元の `flask_elastic/tree/frontback` と同じ操作感で、検索対象を Trino / Iceberg に置き換えています。

## 起動

```bash
cp .env.example .env
docker compose up -d --build
```

ブラウザで http://localhost:8081 を開きます。

フロントエンドの CSS / JavaScript は Docker イメージのビルド時に内容ハッシュ付きのファイル名へ変換され、HTML の参照先も自動更新されます。ファイル名やキャッシュ更新用のクエリ文字列を HTML に手動で反映する必要はありません。

外向けに公開するポートはフロントエンドの `8081` だけです。各バックエンドは Compose の内部ネットワークからのみ接続でき、Nginx 経由で利用します。

| Service | URL |
| --- | --- |
| frontend | http://localhost:8081 |

フロントエンド画面の Backend セレクトで検索に使うバックエンドを切り替えられます。

- トップ画面: http://localhost:8081/
- ログ検索: http://localhost:8081/search
- 疎通確認: http://localhost:8081/health

## コンテナ構成

各コンテナの役割とファイル構成は、それぞれのREADMEに記載しています。

- [frontend](frontend/Readme.md)
- [backend-python](backend-python/Readme.md)
- [backend-go](backend-go/Readme.md)
- [backend-java](backend-java/Readme.md)
- [backend-php](backend-php/Readme.md)
- [backend-ruby](backend-ruby/Readme.md)
- [backend-elixir](backend-elixir/Readme.md)
- `access-log-api`: Nginxが永続化したアクセスログの参照API

## 前提

Trino / Iceberg / ログ収集基盤はこの Compose には含めません。デフォルトでは `trino1:8080` の Trino に接続します。

初回起動前にサンプルをコピーし、環境に合わせて `.env` を編集してください。設定を省略した場合もサンプルと同じ既定値で動作します。

```bash
cp .env.example .env
```

`TRINO_HOST_ALIAS` と `TRINO_HOST_IP` は Compose の `extra_hosts` に使われます。`TRINO_URL` のホスト名を変更する場合は、`TRINO_HOST_ALIAS` も同じ名前にしてください。

## フロントエンドのsyslog転送

`frontend` は、標準出力・標準エラーをDockerホストの
`127.0.0.1:514/TCP`へ送信するように設定されています。
この設定を使用する場合は、コンテナを起動する前にホスト上でrsyslogなどを
TCP 514番で待ち受けさせてください。

受信先が動作していない場合、Dockerのlogging driverを初期化できず、
`frontend`コンテナの作成または起動に失敗することがあります。

Ubuntu / Debianでrsyslogを使用する場合の設定例です。

```conf
# /etc/rsyslog.d/10-docker-frontend.conf
module(load="imtcp")
input(type="imtcp" address="127.0.0.1" port="514")

if $programname == "trino-search-frontend" then {
    action(type="omfile" file="/var/log/trino-search-frontend.log")
    stop
}
```

設定反映後、TCP 514番の待ち受けを確認します。

```bash
sudo systemctl restart rsyslog
sudo ss -lntp | grep ':514'
```

syslog転送を使用しない場合は、`docker-compose.yml`の`frontend`から
次の`logging`設定を削除してから起動してください。

```yaml
    logging:
      driver: syslog
      options:
        syslog-address: "tcp://127.0.0.1:514"
        tag: "{{.Name}}"
```

Nginxのアクセスログはsyslogとは別に名前付きボリューム
`frontend-access-logs`へ保存されるため、この設定を削除しても
画面からのアクセスログ参照やCSVダウンロードは利用できます。

## 前提テーブル

デフォルトでは以下の Trino テーブルを検索します。

- `iceberg.logs.syslog_events`
- `iceberg.logs.authlog_events`

検索と表示に使うカラムは以下です。

- `ts`: ログ時刻
- `host`: ホスト名
- `program`: プログラム名
- `message`: メッセージ

画面では対象日と開始・終了時刻をJSTで指定できます。検索結果は総件数付きでページングされ、1ページあたり10・25・50・100件から選択できます。

## フロントエンド機能

- 本日のログ総数と最近のログを5秒ごとに自動更新するトップ画面
- 対象日、時間帯、ログ種別、ホスト、プログラム、メッセージ検索
- ホスト・プログラムの正規表現検索（例: `/^web\d+$/`。`/` で囲まない場合は完全一致）
- メッセージ検索を主入力とし、その他の条件を折りたためる検索フォーム
- 検索結果のログ種別、ホスト名、プログラム名をクリックした絞り込み
- 総件数表示とページング
- 10・25・50・100件の表示件数切り替え
- 検索語のハイライト
- ログ詳細ダイアログ
- 表示中ログのCSVダウンロード
- URLへの検索条件保存と共有
- Backend選択のLocal Storage保存
- 6バックエンドとTrinoの稼働状況表示
- 稼働状況の5秒ごとの自動更新
- Nginxアクセスログの日次保存・稼働状況画面での閲覧・CSVダウンロード
- デスクトップ／モバイル対応

## API

フロントエンド経由の検索（画面と同様にGETで検索条件を指定）:

```bash
curl "http://localhost:8081/api/flask/logs?date=2026-06-19&time_from=09%3A00&time_to=10%3A30&message=timeout&log_type=syslog&page=1&size=25"
```

POSTにも対応しています:

```bash
curl -X POST http://localhost:8081/api/flask/logs \
  -H "Content-Type: application/json" \
  -d '{
    "date":"2026-06-19",
    "time_from":"09:00",
    "time_to":"10:30",
    "message":"timeout",
    "log_type":"syslog",
    "page":1,
    "size":25
  }'
```

レスポンスには `total`、`page`、`size`、`total_pages`、`logs` が含まれます。

ヘルスチェック:

```bash
curl http://localhost:8081/health/flask
```

アクセスログ:

```bash
curl "http://localhost:8081/api/access-logs?tail=100"
curl "http://localhost:8081/api/access-logs?date=2026-06-23&full=1"
```

ログはJSTの日付ごとに名前付きボリュームへ保存され、標準では14日後に削除されます。
`ACCESS_LOG_RETENTION_DAYS`で保持日数、`ACCESS_LOG_HOST`で表示上のホスト名を変更できます。
`/health`、`/api/access-logs`、静的アセットなどの自動リクエストは記録対象外です。

## 設定

`.env` の環境変数で接続先やテーブル名を変更できます。利用できる変数と既定値は `.env.example` にあります。

- `TRINO_URL`: Trino coordinator の URL
- `TRINO_USER`: Trino に渡すユーザー名
- `TRINO_PASSWORD`: Basic 認証が必要な場合のパスワード
- `TRINO_CATALOG`: Trino catalog
- `TRINO_SCHEMA`: Trino schema
- `TRINO_SYSLOG_TABLE`: syslog 検索対象テーブル
- `TRINO_AUTHLOG_TABLE`: authlog 検索対象テーブル
- `TRINO_TIMESTAMP_COLUMN`: ログ時刻カラム
- `TRINO_TIMESTAMP_EXPRESSION`: ログ時刻の SQL 式。指定時は `TRINO_TIMESTAMP_COLUMN` より優先
- `TRINO_HOST_ALIAS`: `extra_hosts` に登録する Trino のホスト名
- `TRINO_HOST_IP`: `extra_hosts` に登録する Trino のIPアドレス

時刻カラムが文字列などでそのまま比較できない場合は、`TRINO_TIMESTAMP_EXPRESSION` に Trino の SQL 式を設定できます。

```dotenv
TRINO_TIMESTAMP_COLUMN=ts
TRINO_TIMESTAMP_EXPRESSION=CAST("ts" AS timestamp)
```

## テスト

参照元の `flask_elastic/tree/frontback` と同じく、Compose の `test` profile で全バックエンドの共通 API 契約を確認します。

```bash
docker compose --profile test run --rm backend-contract-tests
```

このテストでは Python / Flask, Go, Java, PHP, Ruby, Elixir の `/`, `/health`, `/api/options` の共通レスポンスを確認します。
実際に Trino へ検索する `/api/logs` の契約テストは通常スキップされます。Trino に接続できる環境では以下で有効化できます。

```bash
RUN_SEARCH_CONTRACT_TESTS=1 docker compose --profile test run --rm backend-contract-tests
```

## おまけ情報

`omake/readme1.md` には、本サイトの Nginx アクセスログをアクセスログAPIから取得し、
Elasticsearch のデータストリーム `logs-access-trino` へ日次で取り込むための補足手順をまとめています。
本体の起動には必須ではありませんが、アクセスログを Elasticsearch 側で保管・検索したい場合に参照してください。

主な内容は、取り込みホストから本サイトへ接続するためのホスト設定、
Elasticsearch の ILM ポリシーとインデックステンプレート作成、
アクセスログ取り込みシェルの設置、手動実行、cron による定期実行です。
