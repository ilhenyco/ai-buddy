FROM python:3.12-slim
COPY --from=ghcr.io/astral-sh/uv:0.13.8 /uv /usr/local/bin/uv
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY pyproject.toml uv.lock ./
COPY homych_knowledge ./homych_knowledge
RUN uv sync --frozen --no-dev
COPY data ./data
COPY smoke.py ./smoke.py
USER 65532:65532
ENV PATH="/app/.venv/bin:$PATH"
EXPOSE 8090
CMD ["python", "-m", "homych_knowledge.app"]
