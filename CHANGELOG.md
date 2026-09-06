# Changelog

All notable changes to the Koba core are documented in this file. The
record follows the curated conceptual line, one concept per release.

## [1.0.0] (2026-08-24)

An API is described, not routed. Operations are declared as data, the
execution plan is derived deterministically from that description, and
transport artifacts are generated from the plan rather than wired by hand.
Routing knowledge leaves the domain: the description becomes the source of
truth that every consumer reads.

## [1.1.0] (2026-09-04)

What is true of an operation's boundary is stated beside it as a value.
Typed requests, per-field rules, and the scopes a caller must hold live in
the contract instead of in handlers or middleware conventions, and the
contract is checked against the signature it serves.

## [1.2.0] (2026-09-06)

The contract reaches every wire. Subject binding supplies the authenticated
caller as a typed input; directional socket contracts describe a WebSocket
conversation; Kafka consumers are compiled from the same operation plan.
Response and message companions extend typed field descriptions to values
returned or exchanged, while transport configuration remains explicit.
