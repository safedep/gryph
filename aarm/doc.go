// Package aarm implements Gryph's security layer aligned with the AARM
// (Agentic Action and Resource Management) framework — see https://aarm.dev.
//
// AARM separates governance of agent → tool interactions into discrete
// components: Action Mediation (intercept and normalise), Policy Decision
// Point (evaluate rules), Policy Enforcement Point (apply decisions),
// Context Accumulator (per-session memory), Receipt Generator (audit
// records), and Approval Service. This package contains Gryph's
// implementation of those components, exposed to the rest of the codebase
// as a single security.Check via aarm.NewMediator.
//
// The full specification lives in docs/security-spec.md.
package aarm
