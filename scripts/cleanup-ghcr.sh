#!/usr/bin/env bash
# 清理 GHCR 中旧的 dev-* 镜像版本，只保留最近 KEEP 个；latest 与正式版本号标签不受影响。
# 依赖环境变量：GH_TOKEN、GITHUB_REPOSITORY、PACKAGE、KEEP（默认 3）、DRY_RUN（默认 false）。
set -euo pipefail

: "${GH_TOKEN:?缺少 GH_TOKEN}"
: "${GITHUB_REPOSITORY:?缺少 GITHUB_REPOSITORY}"
: "${PACKAGE:?缺少 PACKAGE}"
KEEP="${KEEP:-3}"
DRY_RUN="${DRY_RUN:-false}"

if ! [[ "$KEEP" =~ ^[0-9]+$ ]]; then
  echo "保留数量必须是整数：$KEEP" >&2
  exit 1
fi

PACKAGE_LC=$(printf '%s' "$PACKAGE" | tr '[:upper:]' '[:lower:]')
API="/repos/${GITHUB_REPOSITORY}/packages/container/${PACKAGE_LC}/versions"

probe_err=$(mktemp)
if ! gh api "${API}?per_page=1" >/dev/null 2>"$probe_err"; then
  if grep -q "Not Found" "$probe_err"; then
    echo "GHCR 包尚不存在或未关联本仓库，跳过清理"
    rm -f "$probe_err"
    exit 0
  fi
  cat "$probe_err" >&2
  rm -f "$probe_err"
  exit 1
fi
rm -f "$probe_err"

# 只挑“所有标签都是 dev-*”的版本：latest / 版本号 / 混合标签 / 无标签版本一律不动。
ids=$(gh api --paginate --slurp "${API}?per_page=100" | jq -r --argjson keep "$KEEP" '
  add
  | map(select(
      (.metadata.container.tags | length) > 0
      and all(.metadata.container.tags[]; startswith("dev-"))
    ))
  | sort_by(.created_at) | reverse
  | .[$keep:]
  | .[]
  | .id
')

if [ -z "$ids" ]; then
  echo "没有超出保留数量的 dev 版本（保留最近 $KEEP 个）"
  exit 0
fi

count=$(printf '%s\n' "$ids" | wc -l | tr -d ' ')
echo "命中 $count 个待清理的 dev 版本（保留最近 $KEEP 个）"

while IFS= read -r id; do
  [ -z "$id" ] && continue
  if [ "$DRY_RUN" = "true" ] || [ "$DRY_RUN" = "1" ]; then
    echo "dry-run: 将删除 version $id"
  else
    gh api -X DELETE "${API}/${id}"
    echo "已删除 version $id"
  fi
done <<< "$ids"
