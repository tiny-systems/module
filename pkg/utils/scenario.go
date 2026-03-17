package utils

import (
	"github.com/tiny-systems/module/api/v1alpha1"
)

// ScenarioPortsFromTrace extracts per-port sample data from a trace.
// Returns a slice ready for TinyScenarioSpec.Ports.
// Ports with nil data (traced but empty payload) are included to distinguish
// "port was traced but empty" from "port not in scenario" for fallback logic.
func ScenarioPortsFromTrace(trace *TraceData) []v1alpha1.ScenarioPortData {
	_, runtimeData := ExtractTraceStatistics(trace)
	if len(runtimeData) == 0 {
		return nil
	}

	ports := make([]v1alpha1.ScenarioPortData, 0, len(runtimeData))
	for port, data := range runtimeData {
		ports = append(ports, v1alpha1.ScenarioPortData{
			Port: port,
			Data: data,
		})
	}
	return ports
}

// RuntimeDataFromScenario converts a TinyScenario's Ports slice into a
// map[string][]byte suitable for passing to SimulatePortDataFromMaps as runtimeData.
// Returns nil if the scenario has no ports (meaning: fall back to schema mock data).
func RuntimeDataFromScenario(scenario *v1alpha1.TinyScenario) map[string][]byte {
	if scenario == nil || len(scenario.Spec.Ports) == 0 {
		return nil
	}

	runtimeData := make(map[string][]byte, len(scenario.Spec.Ports))
	for _, p := range scenario.Spec.Ports {
		runtimeData[p.Port] = p.Data
	}
	return runtimeData
}
