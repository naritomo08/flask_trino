FROM ruby:3.3-slim

ENV DEBIAN_FRONTEND=noninteractive
ENV RACK_ENV=production

WORKDIR /app

RUN apt-get update \
  && apt-get install -y --no-install-recommends build-essential \
  && rm -rf /var/lib/apt/lists/*

COPY Gemfile .
RUN bundle install

COPY . .

EXPOSE 5000

CMD ["bundle", "exec", "rackup", "--host", "0.0.0.0", "--port", "5000"]
