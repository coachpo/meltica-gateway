# Feature Specification: Refactor Streaming Routing Flow to Dispatcher-Conductor Architecture

**Feature Branch**: `001-refactor-the-streaming`  
**Created**: 2025-10-12  
**Status**: Draft  
**Input**: Refactor the streaming routing flow to a Dispatcher + Conductor Meltica architecture with canonical-first ingestion, Binance Adapter ownership of parsing, explicit control plane, and no backward compatibility guarantees.

## Clarifications

### Session 2025-10-12

- Q: What SLA should govern Dispatcher propagation of subscribe/unsubscribe control updates? → A: Within 2 seconds

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Canonical Event Delivery for Consumers (Priority: P1)

Consumers of Meltica market data need a reliable stream of canonical events sourced from Binance without dealing with raw exchange payloads.

**Why this priority**: Canonical delivery is the core promise of the refactor—without it, downstream CLIs/services cannot operate.

**Independent Test**: Trigger Binance WS and scheduled REST updates in isolation; verify consumers receive canonical Meltica Events on the Data Bus with the expected routing metadata and payload.

**Acceptance Scenarios**:

1. **Given** a Binance WebSocket ticker frame arrives, **When** the Binance Adapter normalizes it, **Then** the Dispatcher routes a canonical Meltica Event to the Conductor and the Data Bus delivers it to subscribed consumers.
2. **Given** a scheduled REST poll returns an order book snapshot, **When** the Binance Adapter emits the structured payload, **Then** the Dispatcher canonicalizes and forwards it so consumers receive a Meltica Event tagged with the originating REST function.

---

### User Story 2 - Dynamic Subscription Control (Priority: P2)

Operations teams must adjust live subscriptions by event type or instrument without redeploying services.

**Why this priority**: Dynamic control keeps costs manageable and enables targeted monitoring during incidents.

**Independent Test**: Use a control client to issue subscribe/unsubscribe commands; confirm the Dispatcher updates its dispatch table and native Binance WS/REST schedules accordingly while emitting audit confirmations.

**Acceptance Scenarios**:

1. **Given** a consumer submits a subscription for a canonical event type and filter, **When** the Control Bus forwards the request, **Then** the Dispatcher registers the mapping and initiates the required native WS/REST subscriptions within the defined SLA.
2. **Given** a consumer unsubscribes, **When** the Dispatcher processes the control request, **Then** the dispatch table entry is removed and unnecessary native subscriptions are cancelled so no further events for that filter are delivered.

---

### User Story 3 - Orchestrated Event Fusion (Priority: P3)

Product analytics requires enriched events that merge WS deltas with REST snapshots for accuracy and throttled delivery.

**Why this priority**: Orchestration ensures downstream analytics receive consistent, deduplicated state even when data originates from multiple sources.

**Independent Test**: Simulate concurrent WS deltas and REST snapshots; confirm the Conductor consults the Snapshot Cache, applies throttling rules, and publishes a fused canonical Meltica Event to the Data Bus.

**Acceptance Scenarios**:

1. **Given** a delta event and an outdated snapshot exist, **When** the Conductor processes the canonical event, **Then** it refreshes the Snapshot Cache and emits a reconciled Meltica Event reflecting the latest book state.
2. **Given** multiple events arrive within the throttling window, **When** the Conductor applies orchestration policies, **Then** only the consolidated Meltica Event is published while intermediate duplicates are suppressed.

### Edge Cases

- Binance Adapter receives an unmapped event type; the Dispatcher must reject it with an observable error without impacting other flows.
- Control Bus delivers a malformed or unauthorized subscription request; the Dispatcher must respond with a failure reason and leave existing mappings untouched.
- Snapshot Cache is temporarily stale or unavailable; the Conductor must continue streaming WS deltas while flagging data quality status.
- Duplicate canonical events arrive due to retries; consumers must observe idempotent outcomes based on event keys and sequencing.

## Requirements *(mandatory)*

**Compatibility Note**: This refactor enables breaking APIs/import paths (CQ-08, GOV-06, EP-08); do not ship shims. Every feature MUST convert raw WS/REST payloads into canonical Meltica Events before any logic (EP-01) and rely on the exchange adapter that owns parsing (EP-02). Treat the Dispatcher dispatch table as routing source of truth (EP-03), keep Meltica Conductor orchestration separate from ingress (EP-04), and route subscribe/unsubscribe traffic through the Control Bus (EP-05). Requirements MUST preserve idempotent canonical events (EP-06) with full trace propagation and latency metrics (EP-07). Maintain `/lib` boundaries for reusable infrastructure (ARCH-01/02) and optional notes may land in `BREAKING_CHANGES_v2.md`.

### Functional Requirements

- **FR-001**: All Binance WebSocket frames and REST responses MUST be parsed exclusively by the Binance Adapter into structured Go representations before any dispatching or orchestration occurs.
- **FR-002**: The Dispatcher MUST maintain a dispatch table that maps canonical Meltica Event types and filters to the native WS topics and REST functions that supply them.
- **FR-003**: The Dispatcher MUST filter, discard, or enrich incoming raw instances according to dispatch table policies before canonicalizing them.
- **FR-004**: Every event forwarded by the Dispatcher MUST be transformed into a canonical Meltica Event carrying stable instrument identifiers, sequencing, and trace context.
- **FR-005**: The Control Bus MUST allow consumers to submit subscribe, unsubscribe, and filter update commands expressed in canonical event terms.
- **FR-006**: Upon receiving control commands, the Dispatcher MUST update the dispatch table and adjust native Binance WS/REST subscriptions to match the active consumer set within 2 seconds.
- **FR-007**: The Meltica Conductor MUST join WS deltas with REST snapshots, apply throttling, and coordinate analytics enrichment using the Snapshot Cache.
- **FR-008**: The Meltica Conductor MUST update Snapshot Cache entries atomically when emitting fused events so subsequent orchestration operates on current state.
- **FR-009**: The Data Bus MUST deliver canonical Meltica Events to downstream CLIs/services with ordering guarantees per instrument.
- **FR-010**: The pipeline MUST emit observability signals (trace IDs, drop/convert/orchestrate/publish latencies) spanning Adapter, Dispatcher, Conductor, Control Bus, and Data Bus components.
- **FR-011**: A smoke test suite MUST verify end-to-end flow from Binance Adapter through Dispatcher and Conductor to the Data Bus, including dynamic subscription changes.

### Assumptions

- Binance remains the sole exchange in scope for this refactor; additional exchanges will adopt the same pattern later.
- Consumers can tolerate short gaps (≤2 seconds) during subscription reconfiguration while native WS/REST subscriptions update.
- Downstream services already understand canonical Meltica Event schemas and do not require additional migration support.

### Key Entities *(include if feature involves data)*

- **Canonical Meltica Event**: Normalized message with instrument key, event type, sequencing, payload, and trace metadata used throughout the pipeline.
- **Dispatch Table Entry**: Configuration linking canonical event types/filters to Binance WS topics, REST endpoints, and filter rules with associated control plane state.
- **Control Bus Request**: Consumer-issued command specifying event type, instrument filters, and desired action (subscribe/unsubscribe/update), including audit metadata.
- **Snapshot Cache Record**: Latest consolidated state for an instrument, including snapshot timestamp, provenance, and orchestration hints for the Conductor.
- **Data Bus Channel**: Delivery lane that fans out canonical Meltica Events to subscribing CLIs/services while preserving ordering guarantees.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of events delivered to the Data Bus originate from canonical Meltica Events validated against the dispatch table.
- **SC-002**: 95% of new subscription or unsubscribe requests take effect (native WS/REST adjusted and confirmation emitted) within 2 seconds.
- **SC-003**: 99% of orchestrated events reach consumers within 1 second of the triggering WS frame or REST poll completion, excluding intentional throttling windows.
- **SC-004**: Smoke testing demonstrates uninterrupted end-to-end flow for at least one hour with zero unhandled errors and complete trace coverage across Adapter, Dispatcher, Conductor, Control Bus, and Data Bus components.
