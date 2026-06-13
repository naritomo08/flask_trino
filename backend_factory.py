import os

from trino_backend import TrinoLogBackend


def create_backend(name=None):
    backend_name = (name or os.getenv("LOG_BACKEND", "trino")).lower()
    if backend_name == "trino":
        return TrinoLogBackend()
    raise RuntimeError(f"Unsupported log backend: {backend_name}")
