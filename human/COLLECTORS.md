# COLLECTORS.md — collector ingestion scaling (deferred)

> **Status: deferred.** We're at ~1,000 endpoints; Fleet's existing `server/worker` + `schedule` + Redis
> handles this comfortably with no new infrastructure. This note captures the verified findings so we can
> revisit when we're ~50×bigger (and, only then, weigh whether that's a rewrite or a BEAM-era problem).

## The one thing to remember now
Collectors write to `host_integration_status` on a cron; the **read path (coverage matrix / host list)
only reads that table**, never calls a vendor API synchronously (the Munki lesson). A slow/failing
provider degrades one cell to `unknown`, never the page. That property already gives us most of the
scaling headroom at 1k.

## Verified primitives Fleet already ships (for when we do scale)
- **Durable retrying job queue:** `server/worker` (Job interface, `QueueJob`, `ProcessJobs`, exponential
  backoff). ⚠️ `worker.Worker` is **NOT safe for concurrent use** (`worker.go:79`) and
  `GetFilteredQueuedJobs` has **no `SKIP LOCKED`** (`jobs.go:48`) — it's a single-instance processor. Do
  **not** try to scale throughput by making it a per-host queue.
- **Distributed-lock cron:** `server/service/schedule` (Locker + `holdLock`; MySQL `locks` table default)
  — guarantees one producer instance per tick.
- **Redis-backed distributed GCRA rate limiter** already in-tree (`server/datastore/redis/ratelimit_store.go`
  + `server/platform/middleware/ratelimit`) — currently for inbound API throttling.
- **Redis distributed lock / KV** (`server/service/redis_lock`), **batch upsert** pattern
  (`INSERT … ON DUPLICATE KEY UPDATE`, `SetOrUpdateMunkiInfo`), **bulk host matching**
  (`HostIDsByIdentifier`). Vendored **NATS/JetStream** (today only a log sink).

## The architecture when we need it (≈50k–100k)
1. **Push-first:** lean on provider push (Bitdefender Event Push, Huntress Svix webhooks) for change
   events + periodic **incremental/delta reconciliation pulls** — O(changes), not O(endpoints/cycle).
2. **Jobs table = control plane** carrying **coarse per-provider/per-page tasks** (e.g. 500 devices/page ≈
   200 jobs for a 100k reconcile), NOT per-host.
3. **The key decoupling:** make the **Redis GCRA rate limiter (per provider) the throughput governor** —
   NOT worker/queue concurrency. A bounded goroutine pool inside each page-fetch job does the concurrent
   fetching, governed by the shared token bucket, so worker scaling is decoupled from negotiated provider
   API limits. This reaches 100k **without new queue infrastructure**.
4. Batch upserts to `host_integration_status`; cache host-matching; per-provider 429 circuit breaker
   (model on the Redis IP-banner). Only graduate to a dedicated stream (Redis Streams / NATS / SQS) if
   throughput genuinely demands it.

*(Source: workflow `wf6jzb74x`, scaling agent + verification — "all five checks pass; only outbound
limiter wiring is new.")*
