package metrics

import (
	"google.golang.org/protobuf/types/known/structpb"
)

func Merge(prev, new *structpb.Struct) error {
	prevMap := prev.AsMap()
	newMap := new.AsMap()

	for entityID, v := range newMap {
		if prevSub, ok := prevMap[entityID]; ok {
			if prevSubMap, ok := prevSub.(map[string]interface{}); ok {
				if newSubMap, ok := v.(map[string]interface{}); ok {
					for newMetric, newValue := range newSubMap {
						// some metric we add
						// some just getting avg
						oldValue := prevSubMap[newMetric]
						if oldValue == nil || newValue == nil {
							continue
						}
						newValueFloat, ok := newValue.(float64)
						if !ok {
							continue
						}
						oldValueFloat, ok := oldValue.(float64)
						if !ok {
							continue
						}
						switch newMetric {
						case MetricNodeRunning.String(), MetricEdgeActive.String():
							// avg
							newSubMap[newMetric] = (oldValueFloat + newValueFloat) / 2
						case MetricEdgeMessageSent.String(), MetricEdgeMessageReceived.String(), MetricNodeMessageSent.String(), MetricNodeMessageReceived.String():
							// sum
							newSubMap[newMetric] = oldValueFloat + newValueFloat
						}
					}
				}
			}
		}
	}
	var err error
	newStruct, err := structpb.NewStruct(newMap)
	if err != nil {
		return err
	}
	*new = *newStruct
	return err
}
