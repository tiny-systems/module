package tools

import "sync"

// InMemoryPositionTracker tracks node positions during a session
// Thread-safe for concurrent tool calls
type InMemoryPositionTracker struct {
	mu        sync.RWMutex
	positions map[string][]NodePosition // flowName -> positions
}

// NewPositionTracker creates a new in-memory position tracker
func NewPositionTracker() *InMemoryPositionTracker {
	return &InMemoryPositionTracker{
		positions: make(map[string][]NodePosition),
	}
}

// RecordPosition records a node's position for a flow
func (t *InMemoryPositionTracker) RecordPosition(flowName, nodeID string, x, y int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.positions[flowName] = append(t.positions[flowName], NodePosition{
		NodeID:   nodeID,
		FlowName: flowName,
		X:        x,
		Y:        y,
	})
}

// GetMaxX returns the maximum X position recorded for a flow
// Returns 0 if no positions recorded
func (t *InMemoryPositionTracker) GetMaxX(flowName string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	maxX := 0
	for _, pos := range t.positions[flowName] {
		if pos.X > maxX {
			maxX = pos.X
		}
	}
	return maxX
}

// GetNextY returns the next available Y position for a given X column
// Spreads nodes vertically when they would be placed at similar X positions
func (t *InMemoryPositionTracker) GetNextY(flowName string, targetX int, columnWidth int) int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	baseY := 150
	ySpacing := 180

	// Count nodes in the same X column (within columnWidth tolerance)
	nodesInColumn := 0
	for _, pos := range t.positions[flowName] {
		if abs(pos.X-targetX) < columnWidth {
			nodesInColumn++
		}
	}

	return baseY + (nodesInColumn * ySpacing)
}

// GetAllPositions returns all recorded positions for a flow
func (t *InMemoryPositionTracker) GetAllPositions(flowName string) []NodePosition {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]NodePosition, len(t.positions[flowName]))
	copy(result, t.positions[flowName])
	return result
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

var _ PositionTracker = (*InMemoryPositionTracker)(nil)
