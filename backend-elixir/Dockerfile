FROM hexpm/elixir:1.17.3-erlang-27.1.2-debian-bookworm-20241016-slim AS build

ENV MIX_ENV=prod

WORKDIR /app

RUN mix local.hex --force && mix local.rebar --force

COPY mix.exs mix.lock ./
RUN mix deps.get --only prod && mix deps.compile

COPY config ./config
COPY lib ./lib

RUN mix release

FROM debian:bookworm-20241016-slim AS app

ENV LANG=C.UTF-8
ENV PORT=5000
ENV TRINO_URL=http://trino1:8080
ENV TRINO_USER=log_search
ENV TRINO_CATALOG=iceberg
ENV TRINO_SCHEMA=logs
ENV TRINO_SYSLOG_TABLE=syslog_events
ENV TRINO_AUTHLOG_TABLE=authlog_events
ENV TRINO_TIMESTAMP_COLUMN=ts
ENV TRINO_TIMESTAMP_EXPRESSION=

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends libstdc++6 openssl ca-certificates \
  && rm -rf /var/lib/apt/lists/*

COPY --from=build /app/_build/prod/rel/elixir_elastic ./

EXPOSE 5000

CMD ["bin/elixir_elastic", "start"]
