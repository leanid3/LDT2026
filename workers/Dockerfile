# Один образ на оба воркера: команда запуска выбирает parse или extract.
# Контекст сборки — корень репозитория (нужен contracts/): docker build -f workers/Dockerfile .
FROM python:3.12-slim
# tesseract — опциональный OCR для сканов (WORKER_OCR=true); без него parse честно помечает страницы без текста.
RUN apt-get update && apt-get install -y --no-install-recommends tesseract-ocr tesseract-ocr-rus \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY workers/pyproject.toml workers/
COPY workers/inspector_workers workers/inspector_workers
RUN pip install --no-cache-dir "./workers[ocr]"
COPY contracts contracts
ENV CONTRACTS_DIR=/app/contracts
USER 65532
ENTRYPOINT ["python", "-m", "inspector_workers"]
CMD ["parse"]
