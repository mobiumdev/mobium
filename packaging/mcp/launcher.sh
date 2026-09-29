#!/bin/sh
# The MCPB bundle's entry point. A bundle names one command per operating
# system and none per architecture, so this picks the build for the machine
# it finds itself on and hands over to it: `mobium mcp`, and whatever the
# client passed.
dir=$(cd "$(dirname "$0")" && pwd)
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64)                build=darwin-arm64 ;;
  Darwin-x86_64)               build=darwin-amd64 ;;
  Linux-x86_64)                build=linux-amd64 ;;
  Linux-aarch64 | Linux-arm64) build=linux-arm64 ;;
  *) echo "mobium: this bundle has no build for $(uname -s) $(uname -m)" >&2; exit 1 ;;
esac
exec "$dir/mobium-$build" "$@"
