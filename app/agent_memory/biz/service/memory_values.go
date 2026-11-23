package service

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/utils"
	pb "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	maxContentBytes = 65_535
	maxReasonBytes  = 65_535
)

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate memory ID: %w", err)
	}
	return id.String(), nil
}

func badRequest(message string) error {
	return &utils.HTTPError{Status: http.StatusBadRequest, Message: message}
}

func validateUserID(userID string) error {
	if strings.TrimSpace(userID) == "" || !utf8.ValidString(userID) || utf8.RuneCountInString(userID) > 128 {
		return badRequest("user_id must contain 1 to 128 characters")
	}
	return nil
}

func validateID(field, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return badRequest(field + " must be a canonical UUID")
	}
	return nil
}

func validateGraphRef(userID, graphID string) error {
	if err := validateUserID(userID); err != nil {
		return err
	}
	return validateID("graph_id", graphID)
}

func validateReason(reason string) error {
	if strings.TrimSpace(reason) == "" || len(reason) > maxReasonBytes {
		return badRequest("reason must be nonempty and fit in 65535 UTF-8 bytes")
	}
	return nil
}

func validateConfidence(confidence float64) error {
	if !(confidence >= 0 && confidence <= 1) {
		return badRequest("confidence must be between 0 and 1")
	}
	return nil
}

func businessTime(field string, input *timestamppb.Timestamp) (*time.Time, error) {
	if input == nil {
		return nil, nil
	}
	if err := input.CheckValid(); err != nil {
		return nil, badRequest(field + " is invalid")
	}
	value := input.AsTime().Truncate(time.Microsecond)
	if value.Year() < 1000 {
		return nil, badRequest(field + " is outside the MySQL date range")
	}
	return &value, nil
}

func protoTime(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}

func graphResponse(graph model.MemoryGraph) *pb.Graph {
	return &pb.Graph{GraphId: graph.GraphID, UserId: graph.UserID, Version: graph.Version}
}

func nodeResponse(node model.MemoryNode) *pb.Node {
	result := &pb.Node{
		NodeId: node.NodeID, GraphId: node.GraphID, Content: node.Content, Confidence: node.Confidence,
		IsValid: node.IsValid, ValidFrom: protoTime(node.ValidFrom), FixTime: protoTime(node.FixTime),
		ExpiresAt: protoTime(node.ExpiresAt), Version: node.Version,
	}
	if node.LatestEvaluationID != nil {
		result.LatestEvaluationId = *node.LatestEvaluationID
	}
	return result
}

func evaluationResponse(evaluation model.MemoryEvaluationHistory) *pb.Evaluation {
	return &pb.Evaluation{
		EvaluationId: evaluation.EvaluationID, NodeId: evaluation.NodeID, GraphId: evaluation.GraphID,
		PreviousEvaluationId: evaluation.PreviousEvaluationID, Confidence: evaluation.Confidence,
		Reason: evaluation.Reason, NodeVersion: evaluation.NodeVersion, EvaluatedAt: timestamppb.New(evaluation.CreatedAt),
	}
}

func edgeResponse(edge model.MemoryEdge) *pb.Edge {
	return &pb.Edge{
		EdgeId: edge.EdgeID, GraphId: edge.GraphID, FromNodeId: edge.NodeID, ToNodeId: edge.PreviousNodeID,
		RelationType: pb.RelationType(edge.RelationType), Reason: edge.Reason, ChangeId: edge.ChangeID,
		GraphVersion: edge.GraphVersion,
	}
}

func syncResponse(event model.MemoryOutbox, graphID string) *pb.SyncEvent {
	return &pb.SyncEvent{
		EventId: strconv.FormatUint(uint64(event.ID), 10), GraphId: graphID, ChangeId: event.ChangeID,
		EventType: pb.OutboxEventType(event.EventType), Status: pb.SyncStatus(event.Status), Attempts: event.Attempts,
		NextAttemptAt: protoTime(event.NextAttemptAt), LeaseUntil: protoTime(event.LeaseUntil), LastError: event.LastError,
	}
}
