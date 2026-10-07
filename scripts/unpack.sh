#!/bin/sh
set -eu
# Extract inspected source archive into a new folder; refuses to overwrite an existing checkout.
[ ! -e source ] || { echo 'source already exists; inspect/remove it before extracting again' >&2; exit 1; }
mkdir source
tar -xzf spark-stream-source.tar.gz -C source
echo 'Source ready in ./source. Follow source/README.md for build and deployment.'
