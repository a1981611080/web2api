set -e

# 1) 先拉远程
git fetch origin --prune

# 2) 对齐远程：远程不存在的本地 tag 删除
for t in $(git tag -l); do
git ls-remote --exit-code --tags origin "refs/tags/$t" >/dev/null 2>&1 || git tag -d "$t"
done

# 3) 找远程最大 tag（仅 vA.B.C）
latest=$(git ls-remote --tags --refs origin \
| awk -F/ '{print $3}' \
| grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
| sort -V \
| tail -n1)

# 4) +1（每段 0-100，超了进位）
if [ -z "$latest" ]; then
A=0; B=0; C=1
else
IFS='.' read -r A B C <<< "${latest#v}"
C=$((C+1))
if [ "$C" -gt 100 ]; then C=0; B=$((B+1)); fi
if [ "$B" -gt 100 ]; then B=0; A=$((A+1)); fi
fi

next="v${A}.${B}.${C}"
echo "latest=${latest:-<none>}, next=$next"

# 5) 创建并推送
git tag "$next"
git push origin "$next"

echo "done: $next"