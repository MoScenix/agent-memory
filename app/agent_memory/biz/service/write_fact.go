package service

import (
	"context"
	"errors"
	"strings"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"
	pb "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
	"gorm.io/gorm"
)

func prepareFact(input *pb.FactInput) (model.MemoryNode, error) {
	if input == nil || strings.TrimSpace(input.GetContent()) == "" || len(input.GetContent()) > maxContentBytes {
		return model.MemoryNode{}, badRequest("fact content must be nonempty and fit in 65535 UTF-8 bytes")
	}
	if err := validateConfidence(input.GetConfidence()); err != nil {
		return model.MemoryNode{}, err
	}
	if err := validateReason(input.GetReason()); err != nil {
		return model.MemoryNode{}, err
	}
	validFrom, err := businessTime("valid_from", input.GetValidFrom())
	if err != nil {
		return model.MemoryNode{}, err
	}
	expiresAt, err := businessTime("expires_at", input.GetExpiresAt())
	if err != nil {
		return model.MemoryNode{}, err
	}
	if validFrom != nil && expiresAt != nil && !expiresAt.After(*validFrom) {
		return model.MemoryNode{}, badRequest("expires_at must be after valid_from")
	}
	return model.MemoryNode{
		Content: input.GetContent(), Confidence: input.GetConfidence(), IsValid: true,
		ValidFrom: validFrom, ExpiresAt: expiresAt, Version: 1,
	}, nil
}

func createFact(ctx context.Context, tx *gorm.DB, graphID string, node *model.MemoryNode, reason string) (model.MemoryEvaluationHistory, error) {
	nodeID, err := newID()
	if err != nil {
		return model.MemoryEvaluationHistory{}, err
	}
	nodes := model.NewMemoryNodeQuery(ctx, tx)
	node.NodeID, node.GraphID = nodeID, graphID
	if err := nodes.Create(node); err != nil {
		return model.MemoryEvaluationHistory{}, err
	}
	evaluation, err := recordEvaluation(ctx, tx, *node, 1, node.Confidence, reason)
	if err != nil {
		return model.MemoryEvaluationHistory{}, err
	}
	if err := nodes.SetInitialEvaluation(graphID, nodeID, evaluation.EvaluationID); err != nil {
		return model.MemoryEvaluationHistory{}, queryError(err, "node")
	}
	node.LatestEvaluationID = &evaluation.EvaluationID
	return evaluation, nil
}

func recordEvaluation(ctx context.Context, tx *gorm.DB, node model.MemoryNode, version uint64, confidence float64, reason string) (model.MemoryEvaluationHistory, error) {
	id, err := newID()
	if err != nil {
		return model.MemoryEvaluationHistory{}, err
	}
	evaluation := model.MemoryEvaluationHistory{
		EvaluationID: id, GraphID: node.GraphID, NodeID: node.NodeID, PreviousEvaluationID: node.LatestEvaluationID,
		NodeVersion: version, Confidence: confidence, Reason: reason,
	}
	err = model.NewMemoryEvaluationHistoryQuery(ctx, tx).Create(&evaluation)
	return evaluation, err
}

func reviseFact(ctx context.Context, tx *gorm.DB, node *model.MemoryNode, confidence float64, reason string, validity *model.NodeValidity) (model.MemoryEvaluationHistory, error) {
	if node.LatestEvaluationID == nil {
		return model.MemoryEvaluationHistory{}, errors.New("node is missing its current evaluation")
	}
	evaluation, err := recordEvaluation(ctx, tx, *node, node.Version+1, confidence, reason)
	if err != nil {
		return model.MemoryEvaluationHistory{}, err
	}
	if err := model.NewMemoryNodeQuery(ctx, tx).ApplyEvaluation(evaluation, validity); err != nil {
		return model.MemoryEvaluationHistory{}, queryError(err, "node")
	}
	node.Version = evaluation.NodeVersion
	node.LatestEvaluationID = &evaluation.EvaluationID
	node.Confidence = confidence
	if validity != nil {
		node.IsValid, node.FixTime, node.ExpiresAt = validity.IsValid, validity.FixTime, validity.ExpiresAt
	}
	return evaluation, nil
}

func enqueueFactChange(ctx context.Context, tx *gorm.DB, node model.MemoryNode, changeID string, expectedGraphVersion uint64, eventType model.OutboxEventType) (model.MemoryOutbox, error) {
	err := model.NewMemoryGraphQuery(ctx, tx).AdvanceVersion(node.GraphID, expectedGraphVersion)
	if err != nil {
		return model.MemoryOutbox{}, queryError(err, "graph")
	}
	return model.NewMemoryOutboxQuery(ctx, tx).Enqueue(node.NodeID, changeID, eventType)
}
