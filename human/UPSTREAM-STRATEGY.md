# Upstreaming to Fleet: how to make proper RFCs and clean, minimal PRs

> How we get our extensions into Fleet core. Grounded in Fleet's *actual* process (ADRs, feature-request
> issues, the OpenSpec rejection). Companion: [RFC-coverage-dashboards-and-bundles.md](./RFC-coverage-dashboards-and-bundles.md)
> (our full design) · [PLUGINS.md](./PLUGINS.md) · [pr-host-integration-status.md](./pr-host-integration-status.md).

## 1. The hard truth: you cannot upstream "a plugin system / app store"

Fleet is a focused product, not a platform vendor. The strongest evidence is **ADR-0010 (OpenSpec,
Rejected)**: Fleet declined a *generic mechanism* because it "adds review surface + maintenance burden with
no in-tree consumer" and "creates a parallel layer that can drift." A plugin framework / app-store loader
hits **every one of those objections harder**: it's a large exported surface, it constrains Fleet's future
refactors, and — critically — it ships with **zero in-tree users** (the plugins are ours, out-of-tree).

So the goal is **not** "land the plugin system." The goal is: land a few **tiny, generic *data* seams**,
each justified by **one concrete user-facing feature with an in-tree consumer**, and keep the framework
(registry, loader, providers, app store) **fork-side**. The "app store" emerges from those stable seams +
our catalog — never from a single upstream "plugins" PR.

**Corollary — what Fleet will realistically never take in core:** dynamic third-party code loading, an
exported provider/ingestion interface with no in-tree producer, a bespoke automation-engine framework, a
config-inheritance engine. These stay in our fork (compiled-in) or run out-of-process against stable APIs.

## 2. Fleet's actual process (use this, not a generic RFC)

1. **Feature-request issue first.** `.github/ISSUE_TEMPLATE/feature-request.md`. It goes through **product
   drafting ("Unpacking the why")**. **The issue is the source of truth** for scope + acceptance criteria.
2. **ADR for architecturally-significant decisions.** `docs/Contributing/adr/NNNN-title.md`, copied from
   `template.md` (Status / Context / Decision / Consequences / Alternatives / References). This **is** the
   "proper RFC" in Fleet. Small features don't need one; introducing a *new generic pattern* (e.g. a
   normalized third-party coverage store) does. Submit the ADR as its own small PR, `Status: Proposed`.
3. **Implementation PR(s).** Fork → branch → PR. Body **must** start from
   `.github/pull_request_template.md` (CI `check-pr-template` enforces it). Add a **changes file** under
   `changes/` (one CHANGELOG line ≈ one PR). Tests + green CI. `Fixes #<issue>`.
4. **Do NOT** bring OpenSpec artifacts or our `human/*.md` design docs into the PR — ADR-0010 is explicit
   that Fleet doesn't want parallel committed spec layers. Our `human/` docs stay in our fork; the upstream
   PR carries only the ADR (if warranted) + code + changes file.

## 3. What makes a PR "clean and minimal" here (checklist)

A PR is minimal-and-mergeable when **every** box is checked:

- [ ] **One capability**, expressible as a single CHANGELOG line.
- [ ] **Has an in-tree consumer** in the same (or an immediately adjacent) PR — a UI card, a host-list
      filter, an API caller. *No exported extension point with zero in-tree users* (the OpenSpec trap).
- [ ] **Additive**, not a contract change — new table/endpoint/column; never alters an existing API shape.
- [ ] **Follows an existing pattern** 1:1 (e.g. `SetOrUpdateMunkiInfo` / the MacAdmins card) so review is
      pattern-matching, not architecture debate.
- [ ] **No new config settings / build tags** unless strictly required (each is GitOps + docs surface).
- [ ] **Small diff**, tests included, changes file present, PR template filled.
- [ ] **Framed as a user feature**, never as "infrastructure for our plugins."

Our **`host_integration_status`** PR (already drafted, [pr-host-integration-status.md](./pr-host-integration-status.md))
is the reference model: new table + read API + one card, modeled on Munki. That's the template every
subsequent PR copies.

## 4. The upstream PR ladder (each rung is independently mergeable)

Ordered by dependency and descending merge-odds. Rungs 1–4 are genuine upstream candidates; everything
below the line is **fork-side** and does not get a Fleet PR.

| # | PR (one feature) | In-tree consumer | ADR? | Merge odds |
|---|---|---|---|---|
| 1 | `host_integration_status` store + read/write API + freshness gate | host-detail card | **Yes** (new generic pattern) | medium |
| 2 | Coverage card on host detail + fleet-wide rollup (`ColumnSpec` icons/labels) | the card itself | no | medium |
| 3 | Host-list **filter by coverage** (`ListHostsByCoverage`) | host-list filter UI | no | medium |
| 4 | *(optional)* `team.parent_id` nullable subteam primitive | GitOps/team scoping | **Yes** (touches authz) | low |
| — | — fork-side below this line — | | | |
| F1 | `HostStatusProvider`/`Collector` + `Runner` + registry | our plugins | — | n/a (fork) |
| F2 | ScreenConnect / Bitdefender / Action1 / Veeam / iDrive360 plugins | — | — | n/a (fork, Apache-2.0 on our GitHub) |
| F3 | Dashboards-as-plugins, Views/Triggers engines, bundle resolver, Org tree | — | — | n/a (fork) |

**Sequencing:** ADR #1 (Proposed) → PR #1 → PR #2 → PR #3, each after the prior merges (or as a stacked
draft). File the **feature-request issue** for #1 first and let it be drafted before writing code. Keep
each PR open for a short review loop; don't batch them.

## 5. How the "app store / third-party dashboards" actually ships

Split the ambition by **who maintains the code and where it runs**:

- **In Fleet core (tiny, generic, ours-to-upstream):** the normalized coverage **data model** + a stable
  **read/write API** + a card that renders it + a filter. That's the *plumbing third parties target* — but
  it's plumbing framed as features, not "a plugin API."
- **Out-of-process (anyone, no core change):** a third party writes coverage cells via the API (like our
  ScreenConnect poller) and/or receives events via webhooks. No dynamic code loading in Fleet — the
  security/support surface Fleet won't own.
- **Compiled-in (our fork):** the provider registry/runner, vendor plugins, dashboard-as-plugin renderer,
  automation engines. This is the "store" — a **catalog we host** of integrations + declarative dashboard
  definitions that target the stable seams. The store is ours; the seams are upstream.

Pitch to Fleet is therefore never "let us add plugins," but "add a generic per-host third-party **coverage
status** (like Munki, but any vendor) and let users see + filter on it" — three small features. Third-party
extensibility is the *emergent consequence* of those stable seams, which we make the case for in the ADR's
Consequences, not the headline.

## 6. Anti-patterns that get a Fleet PR rejected

- Exported interface/registry with no in-tree implementation → "speculative framework" (the OpenSpec
  objection). **Fix:** keep it fork-side; upstream only the data method it writes through.
- A PR that says "plugin", "extension system", "app store", or "framework" in the title. **Fix:** name the
  user feature.
- Modifying an existing endpoint/response shape to add our field → breaks contracts. **Fix:** new endpoint.
- Bundling 3 features + a refactor. **Fix:** one feature per PR; refactors separate and justified.
- Bringing our `human/RFC-*.md` / OpenSpec files into the PR. **Fix:** ADR + code + changes file only.
