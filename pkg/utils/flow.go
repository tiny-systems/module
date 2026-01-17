package utils

import (
	"time"

	"github.com/tiny-systems/module/api/v1alpha1"
)

// Flow editor constants - shared between platform and desktop client
const (
	// EdgeBusyStatKey is the stats metric key for edge activity
	EdgeBusyStatKey = "tiny_edge_busy"

	// EdgeAnimationTimeout is the duration after which an edge is no longer considered "busy"
	// If the edge hasn't had activity within this duration, animation should stop
	EdgeAnimationTimeout = 7 * time.Second
)

// Re-export port constants from v1alpha1 for convenience
const (
	// SettingsHandleID is the special handle ID for node settings port
	SettingsHandleID = v1alpha1.SettingsPort

	// ControlHandleID is the special handle ID for node control port
	ControlHandleID = v1alpha1.ControlPort
)

// EdgeStats represents statistics data for an edge
type EdgeStats struct {
	EdgeBusy float64 `json:"tiny_edge_busy,omitempty"`
}

// IsEdgeAnimated determines if an edge should be animated based on its stats
// Returns true if the edge has been active within EdgeAnimationTimeout
func IsEdgeAnimated(stats map[string]interface{}, now float64) bool {
	if stats == nil {
		return false
	}

	busyTimestamp, ok := stats[EdgeBusyStatKey]
	if !ok {
		return false
	}

	var ts float64
	switch v := busyTimestamp.(type) {
	case float64:
		ts = v
	case int:
		ts = float64(v)
	case int64:
		ts = float64(v)
	default:
		return false
	}

	timeSinceActivity := now - ts
	return timeSinceActivity < EdgeAnimationTimeout.Seconds()
}

// ProcessStatsForElements updates the animated flag for elements based on their stats
// This is a helper for backends to process stats consistently
func ProcessStatsForElements(elements []map[string]interface{}, nowSeconds float64) {
	for _, element := range elements {
		data, ok := element["data"].(map[string]interface{})
		if !ok {
			continue
		}

		stats, ok := data["stats"].(map[string]interface{})
		if !ok {
			continue
		}

		element["animated"] = IsEdgeAnimated(stats, nowSeconds)
	}
}

// CleanElementForExport removes runtime-only fields from an element for export
// Returns a cleaned copy of the element
func CleanElementForExport(element map[string]interface{}, isEdge bool) map[string]interface{} {
	// Create a shallow copy
	cleaned := make(map[string]interface{})
	for k, v := range element {
		cleaned[k] = v
	}

	// Common fields to remove
	delete(cleaned, "events")
	delete(cleaned, "sourceNode")
	delete(cleaned, "targetNode")
	delete(cleaned, "isParent")
	delete(cleaned, "dragging")
	delete(cleaned, "initialized")
	delete(cleaned, "selected")
	delete(cleaned, "resizing")
	delete(cleaned, "computedPosition")
	delete(cleaned, "labelBgStyle")
	delete(cleaned, "handleBounds")

	if isEdge {
		delete(cleaned, "animated")
		delete(cleaned, "sourceX")
		delete(cleaned, "sourceY")
		delete(cleaned, "targetX")
		delete(cleaned, "targetY")

		if data, ok := cleaned["data"].(map[string]interface{}); ok {
			delete(data, "error")
			delete(data, "stats")
		}
	} else {
		if data, ok := cleaned["data"].(map[string]interface{}); ok {
			delete(data, "stats")
			delete(data, "emit")
			delete(data, "blocked")
			delete(data, "status")
			delete(data, "error")
			delete(data, "emitting")
			delete(data, "disabled")
			delete(data, "last_status_update")

			// Clean handle styles
			if handles, ok := data["handles"].([]interface{}); ok {
				for _, h := range handles {
					if hMap, ok := h.(map[string]interface{}); ok {
						delete(hMap, "style")
						delete(hMap, "class")
					}
				}
			}
		}
	}

	return cleaned
}
