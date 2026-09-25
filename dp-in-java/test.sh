# 本地有系统 gradle 就用（国内网络下载 wrapper 发行版易超时），否则走仓库内 wrapper（CI 场景）
if command -v gradle >/dev/null 2>&1; then
  gradle test
else
  ./gradlew test
fi
