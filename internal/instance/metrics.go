package instance

import (
	"fmt"
	"strconv"
	"strings"
)

type Metric int

func (m Metric) String() string {
	return fmt.Sprintf("%v", int(m))
}

const prevMetricPrefix = "prev"

const (
	MetricNodeRunning Metric = iota
	MetricEdgeMessageSent
	MetricEdgeMessageReceived
	MetricNodeMessageSent
	MetricNodeMessageReceived
	MetricEdgeActive
)
const metricSeparator = "|"

func GetMetricKey(entityID string, m Metric) string {
	return fmt.Sprintf("%s%s%d", entityID, metricSeparator, m)
}

func GetPrevMetricKey(entityID string, m Metric) string {
	return fmt.Sprintf("%s%s%s%d", prevMetricPrefix, entityID, metricSeparator, m)
}

func GetEntityAndMetric(key string) (string, Metric, bool, error) {
	var prev bool
	if strings.HasPrefix(key, prevMetricPrefix) {
		prev = true
		key = strings.TrimPrefix(key, prevMetricPrefix)
	}
	parts := strings.Split(key, metricSeparator)
	if len(parts) != 2 {
		return "", 0, prev, fmt.Errorf("invalid key")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, prev, fmt.Errorf("invalid key")
	}
	return parts[0], Metric(m), prev, nil
}
