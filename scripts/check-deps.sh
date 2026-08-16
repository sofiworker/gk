#!/usr/bin/env bash
set -euo pipefail

# gk 模块依赖方向检查（见 docs/superpowers/specs/2026-08-07-gk-module-dependency-policy.md）：
# 能力层（gcache/gconfig/ghttp/glog/gnet/gotel/gresolver/gsd/gsql）之间禁止互相 import；
# gnet 子包属于同一能力族，允许族内引用；基础契约层（契约组
# gerr/gretry/grx/gcompress/gcrypt + 运行时原语组 gpoller）可被任何包引用；
# gpoller 自身仅允许 stdlib + x/sys（2026-08-16 修订）。

MODULE="github.com/sofiworker/gk"
CAPABILITY_FAMILIES=(gcache gconfig ghttp glog gnet gotel gresolver gsd gsql)
BASE_RUNTIME_PRIMITIVES=(gpoller)
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

fail=0
for family in "${CAPABILITY_FAMILIES[@]}"; do
  imports="$(go list -f '{{join .Imports "\n"}}' "./$family/..." 2>/dev/null || true)"
  for imp in $imports; do
    case "$imp" in
      "$MODULE/$family" | "$MODULE/$family/"*) continue ;; # 自身/同族
      "$MODULE/"*) ;;
      *) continue ;;
    esac
    for other in "${CAPABILITY_FAMILIES[@]}"; do
      if [[ "$other" == "$family" ]]; then
        continue
      fi
      if [[ "$imp" == "$MODULE/$other" || "$imp" == "$MODULE/$other/"* ]]; then
        echo "dependency policy violation: $family imports capability package $imp"
        fail=1
      fi
    done
  done
done

# 运行时原语组只允许 stdlib + 非 gk 第三方（x/sys 等）。
for prim in "${BASE_RUNTIME_PRIMITIVES[@]}"; do
  imports="$(go list -f '{{join .Imports "\n"}}' "./$prim/..." 2>/dev/null || true)"
  for imp in $imports; do
    if [[ "$imp" == "$MODULE" || "$imp" == "$MODULE/"* ]]; then
      echo "dependency policy violation: runtime primitive $prim imports gk package $imp"
      fail=1
    fi
  done
done

if [[ "$fail" -ne 0 ]]; then
  echo "dependency policy violation (see docs/superpowers/specs/2026-08-07-gk-module-dependency-policy.md)"
  exit 1
fi
echo "dependency policy OK"
