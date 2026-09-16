# Bootstrapping mantlemint from a terrad data directory

**Date:** 2026-09-16
**Status:** Requirements — ready for planning
**Scope tier:** Deep (feature)

## Problem

Mantlemint can only be initialized from genesis (`sync.go`, `mantlemint/reactor.go` `Init`). On Terra Classic a genesis sync takes weeks. Two consequences:

1. Any mantlemint node loss means weeks of downtime.
2. More damaging day to day: **mantlemint releases ship untested against chain upgrades.** Core releases can be rehearsed with `in-place-testnet` against mainnet state, but there is no way to stand up a mantlemint near a fork barrier, so releases like `v4.0.1-patch.3` are delivered to Allnodes without evidence that mantlemint survives the upgrade and still answers queries correctly.

Only one known FCD provider exists (Allnodes), and their archival snapshot (~5.5 TB) is not practically usable for rehearsal.

## Goal

Stand up a mantlemint at an operator-chosen height in hours, feed it blocks across an upgrade height, and **prove that height-based queries and tx queries still work after the fork** — before shipping to Allnodes.

## Users

The mantlemint maintainer preparing a release. Not a general-purpose operator tool in this iteration.

## Why this is tractable

Verified against the current tree:

- Mantlemint runs in fauxMerkleMode (`sync.go`, `fauxMerkleModeOpt`), so every module store is a plain prefix DB at `s/k:<name>/` over the height-limited DB (`store/rootmulti/store.go`, `StoreTypeDB` → `commitDBStoreAdapter`). There is no IAVL tree to rebuild — importing state is writing flat key/values.
- Mantlemint never computes or verifies an app hash; `Inject` patches it from the block header (`mantlemint/reactor.go`). Nothing rejects imported state — and nothing validates it either.
- `Init` runs genesis only when `lastHeight == 0` (`mantlemint/reactor.go`). Seeding CometBFT state at H makes genesis a no-op.
- A terrad data directory is self-describing: `s/latest` gives the version and `s/<version>` unmarshals to a `CommitInfo` listing every store by name (`store/rootmulti/store.go`, `getCommitInfo`). The importer derives the store list from the data; it does not hardcode one.

## Requirements

**R1 — Import from a data directory.** The importer takes a stopped terrad home and an optional `--height` (default: `s/latest`). It must not require running `terrad`, a matching terrad binary, or a prior `snapshots export`. Rationale: during an upgrade rehearsal the binary under test is exactly the one that may fail to load, so the tool must not depend on it.

**R2 — Store list derived from data.** Stores and their names come from the `CommitInfo` at the target version. Adding or removing a module must not require changing the importer.

**R3 — Leaves written at a single height.** Every imported key/value is written as `s/k:<name>/<key>` through the existing `safe_batch`/`hld` layer at height H, producing the same on-disk shape a normal block inject produces.

**R4 — Loud floor below the import height.** Queries below H must fail with an explicit error. Today `heleveldb.Get` returns `nil` with no error for a height with no record, which the LCD surfaces as an empty 200. Without this, a rehearsal can report success while proving nothing.

**R5 — CometBFT state seeded from the block-producing node.** Wasm blobs under `$HOME/data/wasm` and the CometBFT `state.db` / `blockstore.db` seed at H are copied as explicit steps; they are not in the IAVL stores.

The state seed is load-bearing, not housekeeping. Mantlemint wraps the **stock** CometBFT block executor (`mantlemint/executor.go`), so `validateBlock` (cometbft v0.38.21, `state/validation.go`) runs unmodified and rejects block H+1 unless the seeded state matches it on all of: `ChainID`, `ValidatorsHash`, `NextValidatorsHash`, `LastResultsHash`, `LastBlockID`, and a full `LastValidators.VerifyCommit` over the previous commit's signatures.

The only field exempt from this is `AppHash`, and only because `Inject` overwrites it from the block header before applying (`mantlemint/reactor.go`). That single escape hatch is what makes importing foreign state possible at all; every other field is enforced cryptographically.

**Consequence:** the CometBFT state must come from the same node that will produce the blocks. Under `in-place-testnet` that is the **post-transformation testnet node**, which has a new chain ID and a single replacement validator — not the mainnet node the app state was read from. Seeding mainnet state fails immediately on `ValidatorsHash`, then on `VerifyCommit`, because those signatures are from validators that no longer exist on that chain. The app state (R1-R3) and the CometBFT state can therefore come from different sources, and normally will.

**R6 — Catch-up unchanged.** After import, mantlemint uses the existing block feed (`sync.go`) against an `in-place-testnet` node to sync the band across the upgrade height. No changes to the feed.

**R7 — Differential query harness.** Record a query set against the node's LCD at heights inside the synced band and replay it against mantlemint, diffing responses. Coverage must include the two features that matter: `?height=` queries and the tx index (`/index/tx/by_hash`, `/index/tx/by_height`). The harness runs both before and after the upgrade height.

**R8 — Per-store leaf counts.** The importer reports leaf counts per store and the target height, so a structurally wrong import is visible without waiting for the diff.

## Success criteria

- A mantlemint reaches a chosen pre-fork height from a terrad data directory in hours, not weeks.
- Blocks inject across an upgrade height without panic.
- `?height=` and tx queries inside the synced band match the node's LCD, before and after the fork.
- A query below the import height returns an explicit error, never an empty success.

## Scope boundaries

**In scope:** import from a terrad home the maintainer controls and can stop; arbitrary retained height; floor enforcement; wasm and CometBFT seeding; the diff harness.

**Deferred:** third-party pruned snapshot tarballs from untrusted sources; production disaster recovery; publishing mantlemint snapshots for other operators; reworking `Restore()` in `store/rootmulti/store.go` into a working snapshot-stream sink (the natural generalization if this becomes a public bootstrap tool).

**Outside this product's identity:** an imported node is not an archival node. It has no history below H and must never be presented as a replacement for a genesis-synced mantlemint serving historical queries.

Two distinct gaps exist below H, and they behave differently:

- **State (`?height=`)** — no records exist below H. R4 makes these fail loudly.
- **Tx index** — the indexers only run on blocks mantlemint actually injects (`sync.go`, `indexerInstance.Run`). `/index/tx/by_hash` and `/index/tx/by_height` have no data below H, and **syncing forward to current height never backfills it**. Unlike a normal catch-up gap, this one is permanent.

This is the line between the two use cases. For rehearsal it is harmless, because every query runs inside the synced band above H. For serving real FCD traffic it is disqualifying: the node would answer height queries normally above H while having no tx history below it, which is precisely what users would hit.

Closing that gap would require a separate indexer backfill that replays historical blocks through the tx and block indexers without re-executing state. Tractable — the indexers take a block plus an event collector — but real additional work, and explicitly not in this scope.

**Rejected:** bulk state transfer over RPC. The only key-enumerating path is the ABCI `/subspace` query (`cosmossdk.io/store` `iavl/store.go`), which returns all matching pairs in one unpaginated response — unusable at mainnet scale, and commonly blocked on public RPCs. RPC's role stays as block source and as the diff harness's oracle.

**Rejected:** re-using the genesis path via a state export at H. Running every module's `InitGenesis` over mainnet-scale state is the weeks-long cost being escaped.

## Assumptions and risks

- **Catch-up throughput is unmeasured.** After import, mantlemint backfills H+1 to current over RPC via the existing feed (`block_feed/aggregate.go`, `SyncFromUntil`). Injection throughput is what decides how far back `--height` can usefully be set; measure it on the first run.
- **The harness is the only correctness check.** Mantlemint verifies nothing about imported state (see above). If the query set is thin, the rehearsal is theater. Query-set breadth is a first-class deliverable, not a follow-up.
- The target height must still be retained by the source node's pruning settings. Defaulting to `s/latest` avoids this in the common case.
- **That mantlemint accepts in-place-testnet blocks at all is unverified, and gates everything.** The validator-set swap and chain-ID change performed by `in-place-testnet` are exactly what `validateBlock` checks hardest (see R5). De-risk with a spike before building the importer: run `in-place-testnet`, hand-seed a mantlemint's CometBFT state at H from the post-transformation node, point the feed at it, and confirm block H+1 validates. No importer is needed — `validateBlock` runs before any app state is touched, so the app stores can be empty. A day's work that either clears the whole plan or invalidates it.
- Wasm cache format issues (README Q8) may surface on a freshly imported node and be mistaken for import bugs.

## Open questions

- Which query set constitutes adequate coverage for R7 — sampled live traffic, or a hand-built set targeting modules the upgrade touches?
- Should the floor (R4) be stored in the DB as import metadata, or passed as config at startup?
