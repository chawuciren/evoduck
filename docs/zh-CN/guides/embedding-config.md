# Embedding 配置

本文档只记录当前代码中已经明确存在的 Embedding 相关配置项，不额外推断未直接确认的运行时行为。

## 1. 配置入口

Embedding 相关配置位于：

```yaml
memory:
  long_term:
    vector:
      ...
```

其中 embedder 配置位于：

```yaml
memory:
  long_term:
    vector:
      embedder:
        ...
```

## 2. 当前已确认的字段

```yaml
memory:
  long_term:
    vector:
      enabled: true
      prefetch_limit: 5
      score_threshold: 0.7
      embedder:
        type: openai
        model: text-embedding-3-small
        dimensions: 1536
        api_key: ${EMBEDDING_API_KEY}
        base_url: https://api.openai.com/v1
```

已确认字段：

- `enabled`
- `prefetch_limit`
- `score_threshold`
- `embedder.type`
- `embedder.model`
- `embedder.dimensions`
- `embedder.api_key`
- `embedder.base_url`

## 3. 校验层已确认的约束

当 `memory.long_term.vector.enabled = true` 时，至少要满足：

- `memory.long_term.vector.embedder.type` 不能为空
- `memory.long_term.vector.prefetch_limit` 合法
- `memory.long_term.vector.score_threshold` 合法

## 4. 最小示例

```yaml
memory:
  long_term:
    vector:
      enabled: true
      embedder:
        type: openai
        model: text-embedding-3-small
```

## 5. OpenAI 风格示例

```yaml
memory:
  long_term:
    vector:
      enabled: true
      prefetch_limit: 5
      score_threshold: 0.7
      embedder:
        type: openai
        model: text-embedding-3-small
        api_key: ${EMBEDDING_API_KEY}
        base_url: https://api.openai.com/v1
```

## 6. Ollama 风格示例

```yaml
memory:
  long_term:
    vector:
      enabled: true
      embedder:
        type: ollama
        model: nomic-embed-text
        base_url: http://localhost:11434/v1
        api_key: ""
```

## 7. 如果只是先跑通系统

建议先关闭向量相关配置：

```yaml
memory:
  long_term:
    vector:
      enabled: false
```

等主链路稳定后，再单独调 Embedding 与长期记忆策略。

## 8. 配置建议

由于当前文档只记录代码中已经明确存在的字段，比较稳妥的方式是：

1. 需要独立 Embedding 服务时，显式填写 `api_key` 和 `base_url`
2. 先确认向量功能本身确实要启用，再补充 `dimensions`、阈值与长期记忆策略
3. 如果只是首次上手，优先保持 `enabled: false`
