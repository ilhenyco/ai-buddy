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
# Avito Playbook at the pinned commit inside the image: the first start indexes it without GitHub
# (python -m homych_knowledge.bootstrap, docker-compose.release-ai.yml).
RUN commit="$(python -c "import json; print(json.load(open('data/playbook.lock.json'))['commit'])")" \
    && git clone --quiet --no-checkout https://github.com/avito-tech/playbook.git /opt/playbook \
    && git -C /opt/playbook cat-file -e "${commit}^{commit}" \
    && git config --system --add safe.directory /opt/playbook
ENV RAG_PLAYBOOK_REPO=/opt/playbook
USER 65532:65532
ENV PATH="/app/.venv/bin:$PATH"
EXPOSE 8090
CMD ["python", "-m", "homych_knowledge.app"]
