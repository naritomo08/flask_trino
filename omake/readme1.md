# 本サイトのアクセスログを Elasticsearch へ取り込む

本サイト（`flask_trino`）の Nginx アクセスログをアクセスログAPIから取得し、
Elasticsearch のデータストリーム `logs-access-trino` へ日次で取り込む手順です。
既存のアクセスログ取り込み環境と衝突しないよう、シェル、ILMポリシー、
インデックステンプレート、cron設定にも `trino` 専用名を使用します。

本サイトは標準では次のURLで公開されます。

- サイト: `http://<サイトホスト>:8081`
- アクセスログAPI: `http://<サイトホスト>:8081/api/access-logs`

以下の手順は、Elasticsearchへ接続できるホストで実行してください。

## 前提

- 本サイトが `docker compose up -d --build` で起動済みであること
- 実行ホストに `bash`、`curl`、`jq` がインストールされていること
- 実行ホストから本サイトのポート `8081` と Elasticsearchのポート `9200` へ接続できること
- `/opt/elastic/bin` へファイルを作成できること

Ubuntu／Debianで不足しているコマンドをインストールする例:

```bash
sudo apt-get update
sudo apt-get install -y curl jq
sudo install -d -m 755 /opt/elastic/bin
```

最初に、本サイトのアクセスログAPIを確認します。

```bash
curl -fsS "http://<サイトホスト>:8081/api/access-logs?tail=1" | jq .
```

`status` が `ok` のJSONが返れば利用できます。
`<サイトホスト>` は本サイトを起動しているホスト名またはIPアドレスへ置き換えてください。

## 本サイトへ接続するためのホスト設定

取り込みシェルを本サイトとは別のホストで実行する場合、そのホストから
本サイトのDockerホストを名前解決できるようにします。

本サイトの `frontend` コンテナは、Dockerホストのポート `8081` で公開されています。
外部ホストからはコンテナ名 `trino-search-frontend` ではなく、
DockerホストのIPアドレスまたは名前へ接続してください。

例として、DockerホストのIPアドレスが `192.168.11.18`、
取り込み用の名前を `trino-search` とする場合、取り込みシェルを実行するホストで
次の設定を行います。

```bash
echo "192.168.11.18 trino-search" | sudo tee -a /etc/hosts
```

名前解決とアクセスログAPIへの接続を確認します。

```bash
getent hosts trino-search
curl -fsS "http://trino-search:8081/api/access-logs?tail=1" | jq .
```

以降の `ACCESS_LOG_API_URL` には次の値を使用できます。

```dotenv
ACCESS_LOG_API_URL=http://trino-search:8081/api/access-logs
```

環境別の接続先は次のとおりです。

| 取り込みシェルの実行場所 | `ACCESS_LOG_API_URL` の例 |
| --- | --- |
| 本サイトと同じDockerホスト | `http://127.0.0.1:8081/api/access-logs` |
| 別ホスト | `http://trino-search:8081/api/access-logs` |
| 同じDocker Composeネットワーク内のコンテナ | `http://frontend/api/access-logs` |

DNSでDockerホスト名を解決できる場合や、IPアドレスをURLへ直接指定する場合は、
`/etc/hosts` の追加は不要です。

別ホストから接続する場合は、Dockerホストのファイアウォールでも
TCPポート `8081` への接続を許可してください。

## Elasticsearchの初期設定

14日で削除するILMポリシーと、アクセスログ用データストリームの
インデックステンプレートを作成します。

```bash
sudo tee /opt/elastic/bin/setup_accesslog_trino_datastream.sh >/dev/null <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

ES_URL="${ES_URL:-http://elastic1:9200}"
DATA_STREAM="${DATA_STREAM:-logs-access-trino}"

curl -fsS -X PUT "${ES_URL}/_ilm/policy/logs-access-trino-14d-policy" \
  -H "Content-Type: application/json" \
  -d '{
    "policy": {
      "phases": {
        "hot": {
          "actions": {
            "rollover": {
              "max_age": "1d",
              "max_size": "10gb"
            }
          }
        },
        "delete": {
          "min_age": "14d",
          "actions": {
            "delete": {}
          }
        }
      }
    }
  }'

curl -fsS -X PUT "${ES_URL}/_index_template/logs_access_trino_template" \
  -H "Content-Type: application/json" \
  -d "{
    \"index_patterns\": [\"${DATA_STREAM}\"],
    \"priority\": 2000,
    \"data_stream\": {},
    \"template\": {
      \"settings\": {
        \"index.lifecycle.name\": \"logs-access-trino-14d-policy\"
      },
      \"mappings\": {
        \"properties\": {
          \"@timestamp\": { \"type\": \"date\" },
          \"dt\": { \"type\": \"keyword\" },
          \"service\": { \"type\": \"keyword\" },
          \"host\": { \"type\": \"keyword\" },
          \"container\": { \"type\": \"keyword\" },
          \"remote_addr\": { \"type\": \"ip\" },
          \"method\": { \"type\": \"keyword\" },
          \"uri\": { \"type\": \"keyword\" },
          \"status\": { \"type\": \"integer\" },
          \"body_bytes_sent\": { \"type\": \"long\" },
          \"request_time\": { \"type\": \"float\" },
          \"upstream_addr\": { \"type\": \"keyword\" },
          \"user_agent\": { \"type\": \"text\" }
        }
      }
    }
  }"

echo "[INFO] setup completed: ${DATA_STREAM}"
EOF

sudo chmod 755 /opt/elastic/bin/setup_accesslog_trino_datastream.sh
```

Elasticsearchの接続先を環境に合わせて指定して実行します。

```bash
sudo ES_URL="http://elastic1:9200" \
  /opt/elastic/bin/setup_accesslog_trino_datastream.sh
```

## アクセスログ取り込みシェルの設置

`ACCESS_LOG_API_URL` には本サイトのアクセスログAPIを指定します。

```bash
sudo tee /opt/elastic/bin/import_accesslog_trino_to_es.sh >/dev/null <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

ES_URL="${ES_URL:-http://elastic1:9200}"
ACCESS_LOG_API_URL="${ACCESS_LOG_API_URL:-http://127.0.0.1:8081/api/access-logs}"
DATA_STREAM="${DATA_STREAM:-logs-access-trino}"
WORK_DIR="${WORK_DIR:-/tmp/accesslog-trino-to-es}"

TARGET_DT="${1:-$(date -d 'yesterday' +%F)}"

log() {
  echo "[$(date '+%F %T')] $*"
}

if ! [[ "${TARGET_DT}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]]; then
  log "[ERROR] TARGET_DT must be YYYY-MM-DD: ${TARGET_DT}"
  exit 1
fi

mkdir -p "${WORK_DIR}"

TMP_JSON="${WORK_DIR}/accesslog_trino_${TARGET_DT}.json"
TMP_JSONL="${WORK_DIR}/accesslog_trino_${TARGET_DT}.jsonl"
TMP_BULK="${WORK_DIR}/accesslog_trino_${TARGET_DT}.bulk"
API_URL="${ACCESS_LOG_API_URL}?date=${TARGET_DT}&full=1"

cleanup() {
  rm -f "${TMP_JSON}" "${TMP_JSONL}" "${TMP_BULK}"
}
trap cleanup EXIT

log "[INFO] fetch: ${API_URL}"
curl -fsS "${API_URL}" -o "${TMP_JSON}"

STATUS="$(jq -r '.status // empty' "${TMP_JSON}")"
if [ "${STATUS}" != "ok" ]; then
  log "[ERROR] access log API returned an invalid response."
  cat "${TMP_JSON}"
  exit 1
fi

jq -c --arg dt "${TARGET_DT}" '
  .logs[]
  | select(.dt == $dt)
' "${TMP_JSON}" > "${TMP_JSONL}"

COUNT="$(wc -l < "${TMP_JSONL}" | tr -d ' ')"
log "[INFO] target=${TARGET_DT} log_count=${COUNT}"

if [ "${COUNT}" -eq 0 ]; then
  log "[WARN] no access logs found. skip import."
  exit 0
fi

# 同じ日付を再実行しても重複しないよう、既存データを削除してから登録します。
HTTP_CODE="$(curl -sS -o /dev/null -w "%{http_code}" \
  "${ES_URL}/_data_stream/${DATA_STREAM}" || true)"

if [ "${HTTP_CODE}" = "200" ]; then
  log "[INFO] delete existing documents: dt=${TARGET_DT}"
  curl -fsS -X POST \
    "${ES_URL}/${DATA_STREAM}/_delete_by_query?conflicts=proceed&refresh=true" \
    -H "Content-Type: application/json" \
    -d "{
      \"query\": {
        \"term\": {
          \"dt\": \"${TARGET_DT}\"
        }
      }
    }" >/dev/null
fi

: > "${TMP_BULK}"
while IFS= read -r line; do
  printf '{"create":{"_index":"%s"}}\n' "${DATA_STREAM}" >> "${TMP_BULK}"
  printf '%s\n' "${line}" >> "${TMP_BULK}"
done < "${TMP_JSONL}"

RESULT="$(curl -fsS \
  -H "Content-Type: application/x-ndjson" \
  -X POST "${ES_URL}/_bulk?refresh=true" \
  --data-binary @"${TMP_BULK}")"

ERRORS="$(jq -r '.errors' <<<"${RESULT}")"
ITEMS="$(jq '.items | length' <<<"${RESULT}")"

log "[INFO] bulk_items=${ITEMS} bulk_errors=${ERRORS}"

if [ "${ERRORS}" != "false" ]; then
  log "[ERROR] bulk import failed."
  jq '.items[] | select(.create.error != null)' <<<"${RESULT}"
  exit 1
fi

log "[INFO] import completed."
EOF

sudo chmod 755 /opt/elastic/bin/import_accesslog_trino_to_es.sh
```

## 手動実行

本サイトとElasticsearchの接続先を指定して実行します。

```bash
sudo ACCESS_LOG_API_URL="http://trino-search:8081/api/access-logs" \
  ES_URL="http://elastic1:9200" \
  /opt/elastic/bin/import_accesslog_trino_to_es.sh 2026-06-23
```

日付を省略すると前日分を取り込みます。

```bash
sudo ACCESS_LOG_API_URL="http://trino-search:8081/api/access-logs" \
  ES_URL="http://elastic1:9200" \
  /opt/elastic/bin/import_accesslog_trino_to_es.sh
```

取り込み結果の確認:

```bash
curl -fsS "http://elastic1:9200/logs-access-trino/_search?size=1&sort=%40timestamp%3Adesc" | jq .
```

## cronによる定期実行

接続先を `/etc/default/accesslog-trino-to-es` に保存します。

```bash
sudo tee /etc/default/accesslog-trino-to-es >/dev/null <<'EOF'
ACCESS_LOG_API_URL=http://trino-search:8081/api/access-logs
ES_URL=http://elastic1:9200
DATA_STREAM=logs-access-trino
EOF
```

毎日午前0時5分に前日分を取り込む例:

```bash
sudo tee /etc/cron.d/accesslog_trino_to_es >/dev/null <<'EOF'
SHELL=/bin/bash
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

5 0 * * * root set -a; . /etc/default/accesslog-trino-to-es; set +a; /opt/elastic/bin/import_accesslog_trino_to_es.sh >> /var/log/import_accesslog_trino_to_es.log 2>&1
EOF

sudo chmod 644 /etc/cron.d/accesslog_trino_to_es
```

cronを設定する前に、同じ環境変数を使った手動実行が成功することを確認してください。

## 補足

- 本サイトのアクセスログはJSTの日付単位で保存されます。
- `/health`、`/api/access-logs`、CSS、JavaScript、画像などの自動リクエストは記録されません。
- 検索操作はGETで行われるため、`uri` フィールドから利用したバックエンドや検索条件を確認できます。
- 本サイト側のアクセスログ保持期間は `.env` の `ACCESS_LOG_RETENTION_DAYS` で変更できます。
- 取り込み対象日のログが本サイト側ですでに削除されている場合、復元はできません。
- `ACCESS_LOG_API_URL` のホスト名を解決できない場合、`curl: (6) Could not resolve host` となり、手動実行とcron取り込みの両方が失敗します。
