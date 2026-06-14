defmodule ElixirElastic.Application do
  @moduledoc false

  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Plug.Cowboy,
       scheme: :http,
       plug: ElixirElastic.Router,
       options: [port: Application.fetch_env!(:elixir_elastic, :port)]}
    ]

    opts = [strategy: :one_for_one, name: ElixirElastic.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
