#!/bin/bash

docker service rm proxit
docker build -t proxit .
docker service create --name proxit --network cloud --limit-memory 30M -p 80:80  -p 443:443  proxit