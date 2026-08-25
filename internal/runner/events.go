package runner

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	runnerv1 "github.com/Lin-xun1113/SkillGate/gen/go/runner/v1"
	"github.com/Lin-xun1113/SkillGate/internal/identity"
)

type EventRecord struct {
	EventID        string    `json:"event_id"`
	TrialID        string    `json:"trial_id"`
	LogicalTrialID string    `json:"logical_trial_id"`
	ExperimentID   string    `json:"experiment_id"`
	AttemptNo      int32     `json:"attempt_no"`
	Sequence       int64     `json:"sequence"`
	EventType      string    `json:"event_type"`
	OccurredAt     time.Time `json:"occurred_at"`
	PayloadJSON    string    `json:"payload_json"`
	PayloadHash    string    `json:"payload_hash"`
}

type TrialEventStream struct {
	mu              sync.RWMutex
	seenEventIDs    map[string]struct{}
	highestSequence int64
	events          []EventRecord
}

type EventManager struct {
	mu     sync.RWMutex
	trials map[string]*TrialEventStream
}

func NewEventManager() *EventManager {
	return &EventManager{
		trials: make(map[string]*TrialEventStream),
	}
}

func (em *EventManager) RecordEvent(req *runnerv1.ReportEventRequest) (runnerv1.EventReportStatus, int64, string, error) {
	if req.EventId == "" || req.TrialId == "" || req.Sequence <= 0 {
		return runnerv1.EventReportStatus_EVENT_REPORT_STATUS_REJECTED, 0, "event_id, trial_id and sequence > 0 are required", nil
	}

	// Verify payload hash if provided
	if req.PayloadHash != "" && req.PayloadJson != "" {
		var payloadObj any
		if err := json.Unmarshal([]byte(req.PayloadJson), &payloadObj); err != nil {
			return runnerv1.EventReportStatus_EVENT_REPORT_STATUS_REJECTED, 0, "invalid payload json", err
		}
		expectedHash, err := identity.HashCanonical(payloadObj)
		if err != nil {
			return runnerv1.EventReportStatus_EVENT_REPORT_STATUS_REJECTED, 0, "failed to compute canonical payload hash", err
		}
		if expectedHash != req.PayloadHash {
			return runnerv1.EventReportStatus_EVENT_REPORT_STATUS_REJECTED, 0, fmt.Sprintf("payload hash mismatch: got %s, expected %s", req.PayloadHash, expectedHash), nil
		}
	}

	stream := em.getOrCreateStream(req.TrialId)
	stream.mu.Lock()
	defer stream.mu.Unlock()

	// Check idempotency / duplicate. Reusing an event ID with a different
	// payload is a conflict, not a successful retry.
	if _, exists := stream.seenEventIDs[req.EventId]; exists {
		for _, existing := range stream.events {
			if existing.EventID == req.EventId && existing.PayloadHash != req.PayloadHash {
				return runnerv1.EventReportStatus_EVENT_REPORT_STATUS_REJECTED, stream.highestSequence, "event_id payload hash conflict", nil
			}
		}
		return runnerv1.EventReportStatus_EVENT_REPORT_STATUS_DUPLICATE_IGNORED, stream.highestSequence, "event already recorded", nil
	}

	var status runnerv1.EventReportStatus = runnerv1.EventReportStatus_EVENT_REPORT_STATUS_ACCEPTED
	var msg string = "event accepted"

	// Sequence gap check
	if req.Sequence > stream.highestSequence+1 && stream.highestSequence > 0 {
		status = runnerv1.EventReportStatus_EVENT_REPORT_STATUS_SEQUENCE_GAP
		msg = fmt.Sprintf("sequence gap detected: current sequence %d > highest %d + 1", req.Sequence, stream.highestSequence)
	}

	occurredAt := time.UnixMilli(req.OccurredAtUnixMs).UTC()
	if req.OccurredAtUnixMs == 0 {
		occurredAt = time.Now().UTC()
	}

	record := EventRecord{
		EventID:        req.EventId,
		TrialID:        req.TrialId,
		LogicalTrialID: req.LogicalTrialId,
		ExperimentID:   req.ExperimentId,
		AttemptNo:      req.AttemptNo,
		Sequence:       req.Sequence,
		EventType:      req.EventType,
		OccurredAt:     occurredAt,
		PayloadJSON:    req.PayloadJson,
		PayloadHash:    req.PayloadHash,
	}

	stream.seenEventIDs[req.EventId] = struct{}{}
	if req.Sequence > stream.highestSequence {
		stream.highestSequence = req.Sequence
	}
	stream.events = append(stream.events, record)

	return status, stream.highestSequence, msg, nil
}

func (em *EventManager) getOrCreateStream(trialID string) *TrialEventStream {
	em.mu.Lock()
	defer em.mu.Unlock()
	stream, ok := em.trials[trialID]
	if !ok {
		stream = &TrialEventStream{
			seenEventIDs: make(map[string]struct{}),
			events:       make([]EventRecord, 0),
		}
		em.trials[trialID] = stream
	}
	return stream
}

func (em *EventManager) GetEvents(trialID string) []EventRecord {
	em.mu.RLock()
	stream, ok := em.trials[trialID]
	em.mu.RUnlock()
	if !ok {
		return nil
	}
	stream.mu.RLock()
	defer stream.mu.RUnlock()
	res := make([]EventRecord, len(stream.events))
	copy(res, stream.events)
	return res
}
