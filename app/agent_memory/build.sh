#!/bin/bash
set -e
cd -- "$(dirname -- "$0")"
mkdir -p output/bin
cp script/bootstrap.sh output/bootstrap.sh
chmod +x output/bootstrap.sh
go build -o output/bin/agent-memory .
