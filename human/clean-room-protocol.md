# Clean-room protocol for reimplementing `ee/` features

**Why this exists.** Everything under `ee/` is licensed under the proprietary **Fleet EE License** —
we may **not** copy, translate, distribute, or sell it (see [OSS.md §2](./OSS.md#2-the-licensing-picture)).
To legally rebuild a premium feature in our MIT fork, the implementation must be **independently
created** — written without reference to the `ee/` source. This document defines the wall and how we
enforce it, including for AI coding agents.

> This is a process control, not legal advice. It exists to make our clean-room story defensible.
> The concrete legal sign-off on this process is an open item for IP counsel (OSS.md §2.2).

## The two roles (the "wall")

Every premium rebuild has two sides that must be kept separate. **No person or agent may be on both
sides for the same feature.**

### 1. Spec author (the "clean" describer)
- **May read:** the public `fleet.Service` interface (`server/fleet/`, MIT), the core
  `ErrMissingLicense` **stub signatures** (`server/service/*.go`, MIT), upstream user-facing docs
  (`docs/`, or fleetdm.com public docs), the product's **observed runtime behavior**, and public
  standards (SCIM RFC, Apple/Microsoft/Google MDM protocol docs).
- **May reference for *scoping only* (size/among, not logic):** the metadata already captured in
  OSS.md (e.g. "`ee/server/service/teams.go` is ~2455 lines"). File **sizes and names** are scoping
  facts; their **contents** are not to be transcribed into specs.
- **Produces:** a plain-English **functional spec** describing *what* the feature must do (inputs,
  outputs, states, error cases, which MIT datastore methods it uses) — never *how the `ee/` code does
  it*.
- **Must NOT:** paste, paraphrase, or describe implementation details lifted from `ee/` source.

### 2. Implementer (the "clean" builder)
- **May read:** the functional spec, all MIT code (`server/`, `orbit/`, `frontend/`), the datastore
  interface + MySQL implementations, and public standards.
- **Must NEVER read:** anything under `ee/`. Not for "reference," not "just to check," not the tests.
- **Produces:** the MIT implementation in its natural core location (fill the stub in
  `server/service/…`, or a new `server/…` package).

## Rules for AI agents specifically

1. **Reimplementation agents are launched with an explicit prohibition:** their prompt states
   *"You must not read, open, grep, or reference any file under `ee/`. Work only from the functional
   spec and MIT code. If you believe you need `ee/`, stop and report instead."*
2. **The audit/discovery agents that produced OSS.md DID read `ee/`** (that was legitimate — they were
   on the *read/describe* side). Per the wall, **those findings feed spec authoring, and any agent
   session that read `ee/` for a feature is disqualified from *implementing* that feature.** Fresh
   implementer agents start from the spec only.
3. **Recommended hard enforcement (optional):** during implementation phases, add a permission **deny**
   rule for `ee/**` reads to `.claude/settings.json` so the tool layer refuses access, e.g. deny
   `Read`/`Grep`/`Glob` on `ee/**`. This turns the rule into a guardrail the agent cannot bypass.
   (Not enabled by default because discovery/analysis legitimately needs `ee/`. Toggle it on for
   build sprints — ask and I'll wire it via the update-config skill.)
4. **Every reimplementation records an entry in [`clean-room-log.md`](./clean-room-log.md)**: date,
   feature, spec-author source, implementer identity, and an attestation that `ee/` was not read.

## Workflow for a premium rebuild

```
1. Spec author writes human/specs/<feature>.md  (from MIT + public sources only)
2. Log the spec + author in clean-room-log.md
3. Fresh implementer (new agent session / different engineer) builds from the spec
   — ee/ reads denied for the duration
4. Implementer logs the build + attestation in clean-room-log.md
5. Reviewer (may be anyone) checks the diff against the SPEC, not against ee/
```

## What "independently created" does and doesn't allow

| Allowed | Not allowed |
|---|---|
| Reading the MIT `fleet.Service` interface + stub signatures | Reading the `ee/` method bodies |
| Using the existing MIT datastore/schema (it's ours) | Copying `ee/` SQL or logic |
| Implementing to match observed API behavior | Transcribing `ee/` code or comments |
| Following public standards (SCIM RFC, MDM protocol docs) | Paraphrasing `ee/` from memory of having read it |
| Knowing a file's size/name for scoping | Knowing/using its contents |
