package service

import (
	"slices"
	"testing"
	"time"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"
	pb "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
)

func TestTraceSharesWindowAndExpandsSharedAncestorOnce(t *testing.T) {
	cutoff := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	firstStart, secondStart := cutoff.AddDate(0, 1, 0), cutoff.AddDate(0, 3, 0)
	window := traceWindow(&cutoff, nil, []model.MemoryNode{{ValidFrom: &firstStart}, {ValidFrom: &secondStart}}, time.Now())
	if window == nil || !window.End.Equal(secondStart) {
		t.Fatalf("unexpected shared window: %v", window)
	}

	graph := []model.MemoryEdge{
		{EdgeID: "1", NodeID: "a", PreviousNodeID: "shared"},
		{EdgeID: "2", NodeID: "b", PreviousNodeID: "shared"},
		{EdgeID: "3", NodeID: "shared", PreviousNodeID: "old"},
	}
	outgoing := func(ids []string, gotWindow *model.TimeWindow) ([]model.MemoryEdge, error) {
		if gotWindow != window {
			t.Fatalf("window changed during traversal: %v", gotWindow)
		}
		var edges []model.MemoryEdge
		for _, edge := range graph {
			if slices.Contains(ids, edge.NodeID) {
				edges = append(edges, edge)
			}
		}
		return edges, nil
	}

	resp := &pb.TraceRelationsResponse{}
	got, err := traceOutgoing(resp, []string{"a", "b"}, 4, 0, window, outgoing)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b", "shared", "old"}; !slices.Equal(got, want) {
		t.Fatalf("nodes = %v, want %v", got, want)
	}
	if len(resp.Edges) != len(graph) || resp.Truncated {
		t.Fatalf("unexpected edges or truncation: %v", resp)
	}
}
