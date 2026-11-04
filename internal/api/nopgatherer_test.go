package api

import dto "github.com/prometheus/client_model/go"

// nopGatherer is an empty Prometheus registry for tests that do not read metrics.
type nopGatherer struct{}

func (nopGatherer) Gather() ([]*dto.MetricFamily, error) { return nil, nil }
