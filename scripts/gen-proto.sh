#!/usr/bin/env bash
# Regenerates Go protobuf/gRPC code from ../proto for both modules.
# Requires: protoc, protoc-gen-go, protoc-gen-go-grpc (in PATH).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PATH="$(go env GOPATH)/bin:$PATH"

REG="github.com/open-carrier-network/ocn/registry"
OCN="github.com/open-carrier-network/ocn"

gen_common_mappings() {
  local prefix="$1"
  echo "--go_opt=Mcommon.proto=${prefix}/proto/common"
  echo "--go_opt=Mregistry.proto=${prefix}/proto/registry"
  echo "--go_opt=Mocnserver.proto=${prefix}/proto/ocnserver"
  echo "--go-grpc_opt=Mcommon.proto=${prefix}/proto/common"
  echo "--go-grpc_opt=Mregistry.proto=${prefix}/proto/registry"
  echo "--go-grpc_opt=Mocnserver.proto=${prefix}/proto/ocnserver"
}

echo ">> registry_server (registry + common)"
mkdir -p registry_server/proto
protoc -I proto -I proto/include \
  --go_out=registry_server --go_opt=module="$REG" \
  --go-grpc_out=registry_server --go-grpc_opt=module="$REG" \
  $(gen_common_mappings "$REG") \
  proto/common.proto proto/registry.proto

echo ">> ocnserver (common + registry + ocnserver)"
mkdir -p ocnserver/proto
protoc -I proto -I proto/include \
  --go_out=ocnserver --go_opt=module="$OCN" \
  --go-grpc_out=ocnserver --go-grpc_opt=module="$OCN" \
  $(gen_common_mappings "$OCN") \
  proto/common.proto proto/registry.proto proto/ocnserver.proto

echo ">> done"
