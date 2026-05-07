# Embedding Config

[English](../guides/embedding-config.md) | [简体中文](../zh-CN/guides/embedding-config.md)

This guide covers vector memory and embedder configuration.

## 1. When to Enable Vector Memory

Enable vector memory when you need semantic retrieval over long-term memory or larger knowledge-like context.

Keep it disabled for the first run unless the embedder is already configured and reachable.

## 2. Config Shape

```yaml
memory:
  long_term:
    vector:
      enabled: false
      prefetch_limit: 5
      score_threshold: 0.7
      embedder:
        type: ollama
        model: nomic-embed-text
        dimensions: 768
```

## 3. Fields

- `enabled`: turns vector retrieval on or off.
- `prefetch_limit`: number of candidates to retrieve before filtering.
- `score_threshold`: minimum similarity score.
- `embedder.type`: embedder provider type.
- `embedder.model`: embedding model name.
- `embedder.dimensions`: embedding vector dimensions.

## 4. Operational Notes

- Ensure embedding dimensions match the selected model.
- Do not enable vector memory before the embedder is reachable.
- Keep thresholds conservative until you inspect retrieval quality.
- Prefer local embedders for private memory when possible.

## 5. Troubleshooting

If vector retrieval behaves poorly:

1. Confirm embedder connectivity.
2. Confirm dimensions.
3. Lower or raise `score_threshold` based on observed matches.
4. Reduce noisy memory before tuning retrieval.
