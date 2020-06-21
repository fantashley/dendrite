#!/bin/bash

cd $(git rev-parse --show-toplevel)

docker build -f build/docker/Dockerfile.debug -t matrixdotorg/dendrite:debug .
