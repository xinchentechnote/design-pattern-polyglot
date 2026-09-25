#!/usr/bin/env bash
#
# 一键编译 / 测试 / 打包（Rust | Go | C++ | Java | Python）
#
# 用法:
#   ./build.sh                     # 等价于 all：test + build + package
#   ./build.sh test                # 只跑测试
#   ./build.sh build package       # 依次执行多个阶段
#   ./build.sh test --lang rust    # 只处理一种语言（rust|go|cpp|java|python）
#   ./build.sh all --strict        # 工具链缺失视为失败（CI 用；默认跳过并告警）
#
# 产物输出到 dist/：
#   dist/rust/libdp_in_rust.rlib     Rust release 静态库
#   dist/cpp/libdesign_patterns.a    C++ Release 静态库
#   dist/java/*.jar                  Java jar 包
#   dist/python/*.whl                Python wheel 包
#   （Go 为库工程，无独立打包产物，build 阶段完成编译校验）
#
# 兼容 macOS 自带 bash 3.2：不使用关联数组。
#
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST="$ROOT/dist"

ALL_LANGS="rust go cpp java python"
ALL_PHASES="test build package"
LANGS="$ALL_LANGS"
PHASES=""
SELECTED=""
STRICT=0

usage() { sed -n '3,19p' "$0" | sed 's/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
  case "$1" in
    test|build|package) PHASES="$PHASES $1"; shift ;;
    all)                PHASES="$PHASES $ALL_PHASES"; shift ;;
    -l|--lang)          SELECTED="${2:-}"; shift 2 ;;
    --strict)           STRICT=1; shift ;;
    -h|--help)          usage; exit 0 ;;
    *) echo "未知参数: $1" >&2; usage >&2; exit 2 ;;
  esac
done
[ -z "$PHASES" ] && PHASES="$ALL_PHASES"

if [ -n "$SELECTED" ]; then
  case " $ALL_LANGS " in
    *" $SELECTED "*) LANGS="$SELECTED" ;;
    *) echo "未知语言: $SELECTED（可选: $ALL_LANGS）" >&2; exit 2 ;;
  esac
fi

# ---------- 输出工具 ----------
if [ -t 1 ]; then
  C_BLUE=$'\033[1;34m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_RED=$'\033[31m'; C_OFF=$'\033[0m'
else
  C_BLUE=""; C_GREEN=""; C_YELLOW=""; C_RED=""; C_OFF=""
fi
log()  { printf '%s[build]%s %s\n' "$C_BLUE" "$C_OFF" "$*"; }
ok()   { printf '%s  ✔ %s%s\n' "$C_GREEN" "$*" "$C_OFF"; }
warn() { printf '%s  ⚑ %s%s\n' "$C_YELLOW" "$*" "$C_OFF"; }
fail() { printf '%s  ✘ %s%s\n' "$C_RED" "$*" "$C_OFF"; }

has() { command -v "$1" >/dev/null 2>&1; }

# 结果记录：RESULT__<lang>__<phase>（bash 3.2 兼容，无关联数组）
record() { printf -v "RESULT__$1__$2" '%s' "$3"; }
result() { local v="RESULT__$1__$2"; printf '%s' "${!v:-}"; }

phase_enabled() { case " $PHASES " in *" $1 "*) return 0 ;; *) return 1 ;; esac; }

toolchain_missing() {
  local lang=$1 tool=$2
  if [ "$STRICT" -eq 1 ]; then
    fail "$lang: 缺少 $tool（--strict 模式，视为失败）"
    for p in $PHASES; do record "$lang" "$p" FAIL; done
  else
    warn "$lang: 缺少 $tool，跳过该语言"
    for p in $PHASES; do record "$lang" "$p" SKIP; done
  fi
}

# ---------- Rust ----------
rust() {
  has cargo || { toolchain_missing rust cargo; return; }
  if phase_enabled test; then
    if (cd "$ROOT/dp-in-rust" && {
          if cargo nextest --version >/dev/null 2>&1; then cargo nextest run
          else cargo test
          fi
        }); then
      record rust test PASS
    else
      record rust test FAIL
    fi
  fi
  if phase_enabled build || phase_enabled package; then
    if (cd "$ROOT/dp-in-rust" && cargo build --release); then
      record rust build PASS
    else
      record rust build FAIL
    fi
  fi
  if phase_enabled package; then
    if mkdir -p "$DIST/rust" && cp "$ROOT/dp-in-rust/target/release/libdp_in_rust.rlib" "$DIST/rust/"; then
      record rust package PASS
    else
      record rust package FAIL
    fi
  fi
}

# ---------- Go ----------
go() {
  has go || { toolchain_missing go go; return; }
  if phase_enabled test; then
    if (cd "$ROOT/dp-in-go" && command go test ./...); then
      record go test PASS
    else
      record go test FAIL
    fi
  fi
  if phase_enabled build; then
    if (cd "$ROOT/dp-in-go" && command go build ./... && command go vet ./...); then
      record go build PASS
    else
      record go build FAIL
    fi
  fi
  if phase_enabled package; then
    warn "go: 库工程无独立打包产物（build 阶段已完成编译校验）"
    record go package PASS
  fi
}

# ---------- C++ ----------
cpp() {
  has cmake || { toolchain_missing cpp cmake; return; }
  if phase_enabled test; then
    if (cd "$ROOT/dp-in-cpp" &&
        cmake -S . -B build -DCMAKE_BUILD_TYPE=Debug -DBUILD_TESTING=ON >/dev/null &&
        cmake --build build >/dev/null &&
        (cd build && ctest --output-on-failure)); then
      record cpp test PASS
    else
      record cpp test FAIL
    fi
  fi
  if phase_enabled build || phase_enabled package; then
    if (cd "$ROOT/dp-in-cpp" &&
        cmake -S . -B build/release -DCMAKE_BUILD_TYPE=Release -DBUILD_TESTING=OFF >/dev/null &&
        cmake --build build/release >/dev/null); then
      record cpp build PASS
    else
      record cpp build FAIL
    fi
  fi
  if phase_enabled package; then
    if mkdir -p "$DIST/cpp" && cp "$ROOT/dp-in-cpp/build/release/libdesign_patterns.a" "$DIST/cpp/"; then
      record cpp package PASS
    else
      record cpp package FAIL
    fi
  fi
}

# ---------- Java ----------
java() {
  local dir="$ROOT/dp-in-java"
  local cmd
  if has gradle; then
    cmd="gradle"
  elif [ -x "$dir/gradlew" ]; then
    cmd="./gradlew"
  else
    toolchain_missing java gradle
    return
  fi
  if phase_enabled test; then
    if (cd "$dir" && $cmd --no-daemon test >/dev/null); then
      record java test PASS
    else
      record java test FAIL
    fi
  fi
  if phase_enabled build || phase_enabled package; then
    if (cd "$dir" && $cmd --no-daemon assemble >/dev/null); then
      record java build PASS
    else
      record java build FAIL
    fi
  fi
  if phase_enabled package; then
    if mkdir -p "$DIST/java" && cp "$dir"/lib/build/libs/*.jar "$DIST/java/"; then
      record java package PASS
    else
      record java package FAIL
    fi
  fi
}

# ---------- Python ----------
python() {
  has python3 || { toolchain_missing python python3; return; }
  local dir="$ROOT/dp-in-python" venv="$ROOT/dp-in-python/.venv"
  # venv 自愈：不存在、或缺 python/pip（可能被中断产生残缺 venv）时重建
  if [ ! -x "$venv/bin/python" ] || ! "$venv/bin/python" -m pip --version >/dev/null 2>&1; then
    rm -rf "$venv"
    if ! python3 -m venv "$venv" || ! "$venv/bin/python" -m pip --version >/dev/null 2>&1; then
      if ! "$venv/bin/python" -m ensurepip --upgrade >/dev/null 2>&1; then
        fail "python: venv 引导失败（无法安装 pip）"
        for p in $PHASES; do record python "$p" FAIL; done
        return
      fi
    fi
  fi
  local pypath="$venv/bin/python"
  if phase_enabled test; then
    if "$pypath" -m pip install -q --disable-pip-version-check pytest crcmod &&
       (cd "$dir" && "$pypath" -m pytest checksum_service_test.py -v); then
      record python test PASS
    else
      record python test FAIL
    fi
  fi
  if phase_enabled build || phase_enabled package; then
    if "$pypath" -m pip install -q --disable-pip-version-check build &&
       mkdir -p "$DIST/python" &&
       (cd "$dir" && "$pypath" -m build --wheel --outdir "$DIST/python" >/dev/null); then
      record python build PASS
      record python package PASS
    else
      record python build FAIL
      record python package FAIL
    fi
  fi
}

# ---------- 汇总 ----------
print_summary() {
  local lang phase status exit_code=0
  printf '\n'
  log "结果汇总"
  printf '  %-8s' "语言"
  for phase in $ALL_PHASES; do printf '%-10s' "$phase"; done
  printf '\n'
  for lang in $LANGS; do
    printf '  %-8s' "$lang"
    for phase in $ALL_PHASES; do
      status="$(result "$lang" "$phase")"
      [ -z "$status" ] && status="-"
      case "$status" in
        PASS)  printf '%s%-10s%s' "$C_GREEN"  "PASS"  "$C_OFF" ;;
        SKIP)  printf '%s%-10s%s' "$C_YELLOW" "SKIP"  "$C_OFF" ;;
        FAIL)  printf '%s%-10s%s' "$C_RED"    "FAIL"  "$C_OFF"; exit_code=1 ;;
        *)     printf '%-10s' "-" ;;
      esac
    done
    printf '\n'
  done
  if [ "$STRICT" -eq 1 ]; then
    for lang in $LANGS; do
      for phase in $PHASES; do
        if [ "$(result "$lang" "$phase")" = "SKIP" ]; then
          fail "$lang/$phase: strict 模式不允许跳过"
          exit_code=1
        fi
      done
    done
  fi
  return "$exit_code"
}

mkdir -p "$DIST"
for l in $LANGS; do
  log "======== $l ========"
  "$l"
done
print_summary
