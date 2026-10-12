#!/bin/sh
# plugin link はビルドしないため、ソースがバイナリより新しいときだけビルドしてから起動する
# 毎回 go build すると起動が 0.1〜0.3 秒遅れる
root=$HERDR_PLUGIN_ROOT
bin=$root/bin/launcher

if [ ! -x "$bin" ] || [ -n "$(find "$root" -maxdepth 1 \( -name '*.go' -o -name go.mod -o -name go.sum \) -newer "$bin")" ]; then
  go build -C "$root" -o bin/launcher . || {
    printf 'build failed. press enter to close'
    read -r _
    exit 1
  }
fi

exec "$bin" "$@"
