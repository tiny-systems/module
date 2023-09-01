package metrics

import (
	"fmt"
	"strconv"
	"strings"
)

type Metric int

func (m Metric) String() string {
	return fmt.Sprintf("%v", int(m))
}

const (
	MetricNodeRunning Metric = iota
	MetricEdgeMessageSent
	MetricEdgeMessageReceived
	MetricNodeMessageSent
	MetricNodeMessageReceived
)
const metricSeparator = "|"

func GetMetricKey(entityID string, m Metric) string {
	return fmt.Sprintf("%s%s%d", entityID, metricSeparator, m)
}

func GetEntityAndMetric(key string) (string, Metric, error) {

	parts := strings.Split(key, metricSeparator)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid key")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("invalid key")
	}
	return parts[0], Metric(m), nil
}
