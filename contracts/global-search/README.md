# Global Repository Search Contract

This directory is the static contract source for launcher adapters that call:

```bash
starcat search "<query>" --source all --limit 30
```

It does not add a second runtime SDK. Alfred, uTools, Raycast, and future
launchers should copy these fixtures into their tests and keep their runtime
adapter native to the host platform.

## Files

- `schema-v1.json`: additive JSON Schema for successful output.
- `success-all.json`: merged local and GitHub results.
- `success-local-warning.json`: local success with a GitHub provider warning.
- `empty.json`: successful search with no repositories.
- `error-codes.json`: stable CLI and adapter error codes.

Adapters must reject unknown `schema_version` values, ignore unknown fields
within v1, preserve result ordering, use `primary_source` for presentation, and
open only a validated `open_url`.

## 中文说明

本目录是外部启动器调用 `starcat search` 时使用的静态契约真源。各适配器应复用相同
fixture 做测试，但不得在运行时依赖 CLI 源码仓库，也不得复制搜索、排序、去重、鉴权
或 Pro 判断逻辑。

成功响应允许在 v1 内增加 optional 字段；适配器必须拒绝未知 schema version，同时
忽略 v1 中不认识的新增字段。
