# Failover Test Guide

Use this guide only after reading `docs/agent-testing/README.md` and `docs/agent-testing/guides/environment.md`.

## Evidence

- Record test batch ID.
- Record created item IDs and room IDs.
- Record `/health` before fault, during fault, and after recovery.
- Record cleanup results.
- Do not record DSNs, credentials, passwords, tokens, or reusable secrets.

## Redis Authority Fault

1. Create a room and an active item for the current batch.
2. Confirm `/health` reports `mode=normal_cloud`.
3. Block backend connectivity to cloud Redis.
4. Confirm backend `/livez` remains `200`.
5. Confirm `/health` reports local Redis mode or protected items.
6. Confirm ready rebuilt items accept bids.
7. Confirm protected items reject bids without taking down the service.
8. Restore cloud Redis connectivity.
9. Confirm switchback waits for verification.

## MySQL Runtime Fault

1. Create a room and an active item for the current batch.
2. Block backend connectivity to MySQL while backend pods are already running.
3. Confirm bids are accepted only within the configured 10-second buffering window.
4. Confirm settlement, order creation, deposit refunds, and final winner confirmation are paused.
5. Restore MySQL.
6. Confirm bid-log backlog drains.
7. Confirm settlement resumes only after item verification.
