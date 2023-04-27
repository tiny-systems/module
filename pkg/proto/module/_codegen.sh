#!/bin/bash
mkdir -p ../../api
rm ../../api/server-go/*.pb.go 2>/dev/null
protoc --go_out=../../api/ *.proto
protoc --go-grpc_out=../../api/ *.proto
git add ../../api
