#!/usr/bin/env bash

#  when running locally first time
echo "downloading dependencies..." 
sudo apt install mesa-opencl-icd ocl-icd-opencl-dev gcc git bzr jq pkg-config curl clang build-essential hwloc libhwloc-dev wget -y && sudo apt upgrade -y

GO_MIN_VESION="1.20.0"
GO_VERSION=$(go version | awk '{print $3}')

if [[ -z "$(command -v go)" || "$(printf '%s\n' "$GO_VERSION" "$GO_MIN_VERSION" | sort -V | head -n1)" != "$GO_MIN_VERSION" ]]; then
    echo "downloading go $GO_MIN_VESION..."
    wget -q https://dl.google.com/go/go${GO_MIN_VESION}.linux-amd64.tar.gz | sudo tar -C /usr/local -xzf go${GO_MIN_VESION}.linux-amd64.tar.gz
    echo "export PATH=$PATH:/usr/local/go/bin" >> ~/.bashrc && source ~/.bashrc
    rm go${GO_MIN_VESION}.linux-amd64.tar.gz

    echo "GO VERSION: $(go version) "
else 
    echo "GO VERSION: $(go version) "
fi

echo "setup local knowledge node ...."

export RUSTFLAGS="-C target-cpu=native -g"
export FFI_BUILD_FROM_SOURCE=1


make clean all
make epik-pond
#  change rust-toolchain version 2020-10-05  ===> nightly-2023-02-09
# error log 
# build/params_2k.go:39:9: undefined: policy.SetMinVerifiedDealSize
# make: *** [Makefile:77: epik] Error 1

