# Qtun v0.9

## About

a C-S safe tunnel

## How to run

```
Server:
./qtun --key "hahaha" --listen "0.0.0.0:8080" --ip "10.4.4.2/24" --verbose 1 --server_mode 1

Client:
./qtun --key "hahaha" --remote_addrs "47.52.245.181:8080" --ip "10.4.4.3/24" --verbose 1 -server_mode 0
```
