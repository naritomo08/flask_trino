require "base64"
require "json"
require "net/http"
require "uri"

class TrinoClient
  def initialize(
    base_url,
    user: ENV.fetch("TRINO_USER", "log_search"),
    password: ENV.fetch("TRINO_PASSWORD", ""),
    catalog: ENV.fetch("TRINO_CATALOG", "iceberg"),
    schema: ENV.fetch("TRINO_SCHEMA", "logs")
  )
    base = base_url.end_with?("/") ? base_url : "#{base_url}/"
    @statement_uri = URI.join(base, "v1/statement")
    @user = user
    @password = password
    @catalog = catalog
    @schema = schema
  end

  def ping
    execute("SELECT 1", timeout: 5)
    true
  rescue StandardError
    false
  end

  def execute(sql, timeout: 15)
    response = request(:post, @statement_uri, timeout: timeout, body: sql)
    collect_pages(JSON.parse(response.body), timeout: timeout)
  end

  private

  def collect_pages(body, timeout:)
    rows = []
    columns = []

    loop do
      if body["error"]
        message = body["error"]["message"] || body["error"].to_s
        raise "Trino query failed: #{message}"
      end

      rows.concat(body.fetch("data", []))
      columns = body["columns"].map { |column| column["name"] } if body["columns"] && columns.empty?

      next_uri = body["nextUri"]
      return [rows, columns] unless next_uri

      response = request(:get, URI(next_uri), timeout: timeout)
      body = JSON.parse(response.body)
    end
  end

  def request(method, uri, timeout:, body: nil)
    http = Net::HTTP.new(uri.host, uri.port)
    http.use_ssl = uri.scheme == "https"
    http.open_timeout = timeout
    http.read_timeout = timeout

    request = method == :post ? Net::HTTP::Post.new(uri) : Net::HTTP::Get.new(uri)
    trino_headers.each { |key, value| request[key] = value }
    request.body = body if body

    response = http.request(request)
    raise "Trino HTTP #{response.code}: #{response.body}" unless response.is_a?(Net::HTTPSuccess)

    response
  end

  def trino_headers
    headers = {
      "X-Trino-User" => @user,
      "X-Trino-Source" => "ruby-sinatra-trino-log-search",
      "Content-Type" => "text/plain; charset=utf-8"
    }
    headers["X-Trino-Catalog"] = @catalog unless @catalog.empty?
    headers["X-Trino-Schema"] = @schema unless @schema.empty?
    headers["Authorization"] = "Basic #{Base64.strict_encode64("#{@user}:#{@password}")}" unless @password.empty?
    headers
  end
end
