# Xiaoya BYOA MVP Final Release Handoff

> **STATUS: COMPLETED / RELEASE APPROVED**
>
> 当前事实来源：[`docs/BYOA_MVP_STATUS.md`](BYOA_MVP_STATUS.md)。

## Release Contract

```text
Stable branch: main
Production image: ghcr.io/pixingzoudaiyuexing/xiaoya-byoa:latest
Feature release source: d23062f5
```

PR #1 的 BYOA MVP 已完成本地和 CI 验收，正式发布决策已批准。GitHub Actions 在 `main` 推送后发布 `latest`、`sha-<short-sha>`，版本 tag 可发布对应版本标签。RealityPanel 永远固定拉取 `latest`。

## Completed Acceptance

```text
CI, Docker clean build, empty/persistent volume, if-newer: PASS
Aliyun NEED_AUTH / real QR / video playback / expiration-rescan: PASS
Quark NEED_AUTH / real QR / playback / expiration-rescan: PASS
Previous browser A/B cookie isolation: PASS
Local HTTPS/fallback/Secure Cookie/management hiding: PASS
```

## Accepted Risk

BYOA same-path requests may reuse request-derived Link state through OpenList global Link Cache / singleflight, with potential cross-user playback capability or Quark credential reuse for the exact same path. This is **KNOWN / ACCEPTED / DEFERRED** for the low-traffic Reality camouflage-site use case. The prior A/B acceptance did not cover exact same-path cache reuse. Do not fix this issue in the release task; future hardening is planned.

## Deployment Handoff

Use the release Compose contract in [`docs/BYOA_DEPLOY.md`](BYOA_DEPLOY.md). Keep `/opt/alist/data` across image updates and run `docker compose pull` followed by `docker compose up -d`. Production Reality E2E remains deferred to pre-production deployment acceptance.
