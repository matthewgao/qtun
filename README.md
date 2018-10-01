[![GitHub release](https://img.shields.io/github/release/meshbird/meshbird/all.svg?style=flat-square)](https://github.com/meshbird/meshbird/releases)

# Meshbird 2.0

## About

Meshbird is open-source **cloud-native** multi-region multi-cloud decentralized private networking.

## Install

Download and install latest release from this page https://github.com/meshbird/meshbird/releases.

## How to run

```
Server:
./meshbird --key "hahaha" --listen "0.0.0.0:8080" --ip "10.4.4.2/24" --verbose 1 --server_mode 1

Client:
./meshbird --key "hahaha" --remote_addrs "47.52.245.181:8080" --ip "10.4.4.3/24" --verbose 1 -server_mode 0
```
