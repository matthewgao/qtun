# Qtun v1.0

## About

a C-S safe tunnel, support multi-ip pipeline

## How to run

```
Server:
./qtun qt --key "hahaha" --listen "0.0.0.0:8080" --ip "10.4.4.2/24" --verbose --server_mode

Client:
./qtun qt --key "hahaha" --remote_addrs "8.8.8.80:8080" --ip "10.4.4.3/24" --verbose -server_mode
```
