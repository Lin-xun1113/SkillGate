# Changelog

All notable changes to SkillGate will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

#### M6 - Release Gate (2026-08-26)

Complete policy-driven release gate implementation for automated promotion decisions.

**Core Features:**
- Policy engine with CEL expression evaluation and strict YAML validation
- Trigger evidence aggregation (recall/specificity from autonomous trigger cases)
- Security evidence aggregation (critical/high/confirmed exploit counts)
- Hard gates: REJECT on critical security findings, HOLD on incomplete evidence or CI crossing zero
- Positive gates: PROMOTE when utility lift, routing, reliability, cost, and security thresholds pass
- Immutable metric snapshots with canonical hash-based persistence
- Full decision traceability with policy hash, matched/evaluated rules, failed conditions, and explanations

**Database:**
- New tables: `metrics_snapshots`, `release_decisions`
- Migration: `00005_release_gate.sql`

**Integration:**
- `ProcessExperiment` evaluates policy after report generation
- Reuses persisted snapshots across experiment reruns for consistency
- Projects decisions into `report.json`, `report.md`, `report.html`
- No-policy experiments complete normally without decision

**Documentation:**
- Specification: `docs/comet/specs/release-gate/spec.md`
- Archive: `docs/comet/archive/2026-08-26-m6/`

**Packages:**
- `internal/strategy` - Policy parsing, validation, CEL compilation
- `internal/releasegate` - Snapshot building, hard gates, decision evaluation
- `internal/metrics/routing.go` - Trigger and security evidence aggregation
- `internal/store/postgres/releasegate.go` - Snapshot and decision persistence

## [Previous Releases]

### M5 - Grading Pipeline with Control Plane Integration
- Complete grading pipeline with experiment scheduler
- PostgreSQL persistence for trials, grades, and reports
- Bootstrap CI estimation and pass@k metrics
- Report generation in JSON, Markdown, and HTML formats

### M4 - Native Grading Foundation
- Core grading primitives and trial execution
- Grader registry and executor framework
- Metrics aggregation infrastructure

### M3 - Database Integration
- PostgreSQL store implementation
- Experiment lifecycle management
- Trial result persistence

### M2 - Manifest and Identity
- Experiment manifest schema
- Trial identity generation
- Case pairing logic

### M1 - Project Foundation
- Initial project structure
- Core domain models
- Basic experiment types

### M0 - Bootstrap
- Project initialization
- Development tooling setup
