SHELL = /bin/bash

.DEFAULT_GOAL := download-proofs

download-proofs:
	go run github.com/filecoin-project/go-paramfetch/paramfetch 2048 ./docker-images/proof-parameters.json

build-images:
	docker build -t "iptestground/oni-buildbase:v13-lotus" -f "docker-images/Dockerfile.epik.oni-buildbase" "docker-images"
	docker build -t "iptestground/oni-runtime:v7" -f "docker-images/Dockerfile.epik.oni-runtime" "docker-images"
	docker build -t "iptestground/oni-runtime:v8-debug" -f "docker-images/Dockerfile.epik.oni-runtime-debug" "docker-images"

#	docker build -t "iptestground/oni-buildbase:v13-lotus" -f "docker-images/Dockerfile.oni-buildbase" "docker-images"
#	docker build -t "iptestground/oni-runtime:v7" -f "docker-images/Dockerfile.oni-runtime" "docker-images"
#	docker build -t "iptestground/oni-runtime:v8-debug" -f "docker-images/Dockerfile.oni-runtime-debug" "docker-images"

# push-images:
# 	docker push iptestground/oni-buildbase:v13-lotus
# 	docker push iptestground/oni-runtime:v7
# 	docker push iptestground/oni-runtime:v8-debug

# pull-images:
# 	docker pull iptestground/oni-buildbase:v13-lotus
# 	docker pull iptestground/oni-runtime:v7
# 	docker pull iptestground/oni-runtime:v8-debug

.PHONY: download-proofs build-images push-images pull-images
