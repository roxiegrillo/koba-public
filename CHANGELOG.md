# Changelog

All notable changes to the Koba core are documented in this file. The
record follows the curated conceptual line, one concept per release.

## [1.0.0] (2026-08-24)

An API is described, not routed. Operations are declared as data, the
execution plan is derived deterministically from that description, and
transport artifacts are generated from the plan rather than wired by hand.
Routing knowledge leaves the domain: the description becomes the source of
truth that every consumer reads.
