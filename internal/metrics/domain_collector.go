package metrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"windshift/internal/database"
	"windshift/internal/models"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	collectionTimeout = 2 * time.Second

	// agent_runs and webhook_deliveries grow forever, so the full-table
	// aggregates behind these gauges are refreshed at most once per interval
	// instead of on every scrape.
	domainRefreshInterval = 5 * time.Minute
)

var terminalAgentRunStatuses = []string{
	models.AgentRunStatusSucceeded,
	models.AgentRunStatusFailed,
	models.AgentRunStatusCanceled,
	models.AgentRunStatusKilled,
}

type domainCollector struct {
	db database.Database

	agentRunQueueDepth      *prometheus.Desc
	agentRunsInFlight       *prometheus.Desc
	agentRunOutcomes        *prometheus.Desc
	agentRunDurationAverage *prometheus.Desc
	agentRunDurationSamples *prometheus.Desc
	webhookDispatches       *prometheus.Desc

	refreshInterval time.Duration
	now             func() time.Time

	mu          sync.Mutex
	summary     *domainSummary
	refreshedAt time.Time
}

// domainSummary is immutable once published; readers may hold the pointer
// without the collector lock.
type domainSummary struct {
	agentRunStatusCounts map[string]int64
	agentRunDurations    map[string]agentRunDuration
	webhookCounts        map[bool]int64
}

type agentRunDuration struct {
	samples    int64
	sumSeconds float64
}

func newDomainCollector(db database.Database) prometheus.Collector {
	return &domainCollector{
		db:              db,
		refreshInterval: domainRefreshInterval,
		now:             time.Now,
		agentRunQueueDepth: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "agent", "run_queue_depth"),
			"Current number of queued agent runs.", nil, nil,
		),
		agentRunsInFlight: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "agent", "runs_in_flight"),
			"Current number of running agent runs.", nil, nil,
		),
		agentRunOutcomes: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "agent", "run_outcomes"),
			"Current retained agent runs by terminal outcome.", []string{"outcome"}, nil,
		),
		agentRunDurationAverage: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "agent", "run_duration_average_seconds"),
			"Average duration of retained completed agent runs by outcome.", []string{"outcome"}, nil,
		),
		agentRunDurationSamples: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "agent", "run_duration_samples"),
			"Number of retained completed agent runs included in the duration average.", []string{"outcome"}, nil,
		),
		webhookDispatches: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "webhook", "dispatches"),
			"Current retained webhook deliveries by outcome.", []string{"outcome"}, nil,
		),
	}
}

func (c *domainCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.agentRunQueueDepth
	ch <- c.agentRunsInFlight
	ch <- c.agentRunOutcomes
	ch <- c.agentRunDurationAverage
	ch <- c.agentRunDurationSamples
	ch <- c.webhookDispatches
}

func (c *domainCollector) Collect(ch chan<- prometheus.Metric) {
	summary, err := c.currentSummary()
	if err != nil {
		for _, desc := range []*prometheus.Desc{
			c.agentRunQueueDepth,
			c.agentRunsInFlight,
			c.agentRunOutcomes,
			c.agentRunDurationAverage,
			c.agentRunDurationSamples,
			c.webhookDispatches,
		} {
			ch <- prometheus.NewInvalidMetric(desc, fmt.Errorf("collect domain metrics: %w", err))
		}
		return
	}

	ch <- prometheus.MustNewConstMetric(c.agentRunQueueDepth, prometheus.GaugeValue,
		float64(summary.agentRunStatusCounts[models.AgentRunStatusQueued]))
	ch <- prometheus.MustNewConstMetric(c.agentRunsInFlight, prometheus.GaugeValue,
		float64(summary.agentRunStatusCounts[models.AgentRunStatusRunning]))
	for _, status := range terminalAgentRunStatuses {
		ch <- prometheus.MustNewConstMetric(c.agentRunOutcomes, prometheus.GaugeValue,
			float64(summary.agentRunStatusCounts[status]), status)
	}

	for _, status := range terminalAgentRunStatuses {
		duration := summary.agentRunDurations[status]
		if duration.samples <= 0 {
			continue
		}
		average := duration.sumSeconds / float64(duration.samples)
		if average < 0 {
			average = 0
		}
		ch <- prometheus.MustNewConstMetric(c.agentRunDurationAverage, prometheus.GaugeValue, average, status)
		ch <- prometheus.MustNewConstMetric(c.agentRunDurationSamples, prometheus.GaugeValue,
			float64(duration.samples), status)
	}

	ch <- prometheus.MustNewConstMetric(c.webhookDispatches, prometheus.GaugeValue,
		float64(summary.webhookCounts[true]), "success")
	ch <- prometheus.MustNewConstMetric(c.webhookDispatches, prometheus.GaugeValue,
		float64(summary.webhookCounts[false]), "failure")
}

// currentSummary serves the cached summary until it is older than
// refreshInterval; only then does a scrape pay for the aggregate queries. A
// failed refresh keeps the previous summary so a slow or broken database never
// drops the metrics entirely.
func (c *domainCollector) currentSummary() (*domainSummary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.summary != nil && c.now().Sub(c.refreshedAt) < c.refreshInterval {
		return c.summary, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), collectionTimeout)
	defer cancel()
	summary, err := c.querySummary(ctx)
	if err != nil {
		if c.summary != nil {
			return c.summary, nil
		}
		return nil, err
	}
	c.summary = summary
	c.refreshedAt = c.now()
	return c.summary, nil
}

func (c *domainCollector) querySummary(ctx context.Context) (*domainSummary, error) {
	summary := &domainSummary{
		agentRunStatusCounts: map[string]int64{
			models.AgentRunStatusQueued:    0,
			models.AgentRunStatusRunning:   0,
			models.AgentRunStatusSucceeded: 0,
			models.AgentRunStatusFailed:    0,
			models.AgentRunStatusCanceled:  0,
			models.AgentRunStatusKilled:    0,
		},
		agentRunDurations: map[string]agentRunDuration{},
		webhookCounts:     map[bool]int64{false: 0, true: 0},
	}
	if err := c.queryAgentRunStatusCounts(ctx, summary); err != nil {
		return nil, fmt.Errorf("query agent run status counts: %w", err)
	}
	if err := c.queryAgentRunDurations(ctx, summary); err != nil {
		return nil, fmt.Errorf("query agent run durations: %w", err)
	}
	if err := c.queryWebhookCounts(ctx, summary); err != nil {
		return nil, fmt.Errorf("query webhook counts: %w", err)
	}
	return summary, nil
}

func (c *domainCollector) queryAgentRunStatusCounts(ctx context.Context, summary *domainSummary) error {
	rows, err := c.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM agent_runs GROUP BY status`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return err
		}
		summary.agentRunStatusCounts[status] = count
	}
	return rows.Err()
}

func (c *domainCollector) queryAgentRunDurations(ctx context.Context, summary *domainSummary) error {
	durationExpression := "(julianday(ended_at) - julianday(started_at)) * 86400.0"
	if database.IsPostgresDriver(c.db.GetDriverName()) {
		durationExpression = "EXTRACT(EPOCH FROM (ended_at - started_at))"
	}
	query := fmt.Sprintf(`
		SELECT status, COUNT(*), COALESCE(SUM(%s), 0)
		FROM agent_runs
		WHERE started_at IS NOT NULL AND ended_at IS NOT NULL
		GROUP BY status
	`, durationExpression)
	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status string
		var count int64
		var durationSum float64
		if err := rows.Scan(&status, &count, &durationSum); err != nil {
			return err
		}
		if count <= 0 || !models.IsAgentRunTerminal(status) {
			continue
		}
		summary.agentRunDurations[status] = agentRunDuration{samples: count, sumSeconds: durationSum}
	}
	return rows.Err()
}

func (c *domainCollector) queryWebhookCounts(ctx context.Context, summary *domainSummary) error {
	rows, err := c.db.QueryContext(ctx, `SELECT success, COUNT(*) FROM webhook_deliveries GROUP BY success`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var success bool
		var count int64
		if err := rows.Scan(&success, &count); err != nil {
			return err
		}
		summary.webhookCounts[success] = count
	}
	return rows.Err()
}
