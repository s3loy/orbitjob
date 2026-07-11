-- Migration: SLO/SLI system
-- Creates tables for SLI definitions, SLO targets, pre-aggregated snapshots,
-- error budget tracking, and burn rate alerts.

CREATE TABLE IF NOT EXISTS slis (
    id BIGSERIAL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    name VARCHAR(128) NOT NULL,
    description TEXT,
    sli_type VARCHAR(32) NOT NULL,
    source_type VARCHAR(32) NOT NULL DEFAULT 'check_run',
    source_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    aggregation VARCHAR(32) NOT NULL DEFAULT 'ratio',
    good_event_criteria JSONB NOT NULL DEFAULT '{}'::jsonb,
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_slis_tenant ON slis(tenant_id);
CREATE INDEX IF NOT EXISTS idx_slis_tenant_deleted ON slis(tenant_id, deleted_at) WHERE deleted_at IS NULL;

DROP TRIGGER IF EXISTS trg_slis_updated_at ON slis;
CREATE TRIGGER trg_slis_updated_at
BEFORE UPDATE ON slis
FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE IF NOT EXISTS slos (
    id BIGSERIAL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    name VARCHAR(128) NOT NULL,
    description TEXT,
    sli_id BIGINT NOT NULL REFERENCES slis(id),
    target DECIMAL(5,4) NOT NULL,
    window_type VARCHAR(16) NOT NULL DEFAULT 'rolling',
    window_duration BIGINT NOT NULL DEFAULT 2592000,
    alert_fast_burn_rate DECIMAL(6,2) NOT NULL DEFAULT 14.4,
    alert_slow_burn_rate DECIMAL(6,2) NOT NULL DEFAULT 2.0,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_slos_tenant ON slos(tenant_id);
CREATE INDEX IF NOT EXISTS idx_slos_tenant_status ON slos(tenant_id, status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_slos_sli ON slos(sli_id);

DROP TRIGGER IF EXISTS trg_slos_updated_at ON slos;
CREATE TRIGGER trg_slos_updated_at
BEFORE UPDATE ON slos
FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE IF NOT EXISTS sli_snapshots (
    id BIGSERIAL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    sli_id BIGINT NOT NULL REFERENCES slis(id),
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    good_events_count BIGINT NOT NULL DEFAULT 0,
    total_events_count BIGINT NOT NULL DEFAULT 0,
    sli_value DECIMAL(10,6),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, sli_id, window_start)
);

CREATE INDEX IF NOT EXISTS idx_sli_snapshots_lookup ON sli_snapshots(tenant_id, sli_id, window_start, window_end);
CREATE INDEX IF NOT EXISTS idx_sli_snapshots_window ON sli_snapshots(window_start, window_end);


CREATE TABLE IF NOT EXISTS budgets (
    id BIGSERIAL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    slo_id BIGINT NOT NULL REFERENCES slos(id),
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    budget_total DECIMAL(20,10) NOT NULL,
    budget_consumed DECIMAL(20,10) NOT NULL DEFAULT 0,
    budget_remaining DECIMAL(20,10) NOT NULL,
    burn_rate DECIMAL(10,4),
    status VARCHAR(16) NOT NULL DEFAULT 'healthy',
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, slo_id, window_start)
);

CREATE INDEX IF NOT EXISTS idx_budgets_lookup ON budgets(tenant_id, slo_id, window_start, window_end);
CREATE INDEX IF NOT EXISTS idx_budgets_status ON budgets(tenant_id, status) WHERE status IN ('at_risk', 'exhausted');


CREATE TABLE IF NOT EXISTS budget_alerts (
    id BIGSERIAL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    slo_id BIGINT NOT NULL REFERENCES slos(id),
    budget_id BIGINT NOT NULL REFERENCES budgets(id),
    alert_type VARCHAR(16) NOT NULL,
    burn_rate DECIMAL(10,4) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_budget_alerts_lookup ON budget_alerts(tenant_id, slo_id, status);
CREATE INDEX IF NOT EXISTS idx_budget_alerts_active ON budget_alerts(tenant_id, status) WHERE status = 'active';


-- RLS policies (same pattern as checks)
ALTER TABLE slis ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation_slis ON slis;
CREATE POLICY tenant_isolation_slis ON slis
    USING (tenant_id = current_setting('app.tenant_id', true)::VARCHAR);

ALTER TABLE slos ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation_slos ON slos;
CREATE POLICY tenant_isolation_slos ON slos
    USING (tenant_id = current_setting('app.tenant_id', true)::VARCHAR);

ALTER TABLE sli_snapshots ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation_sli_snapshots ON sli_snapshots;
CREATE POLICY tenant_isolation_sli_snapshots ON sli_snapshots
    USING (tenant_id = current_setting('app.tenant_id', true)::VARCHAR);

ALTER TABLE budgets ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation_budgets ON budgets;
CREATE POLICY tenant_isolation_budgets ON budgets
    USING (tenant_id = current_setting('app.tenant_id', true)::VARCHAR);

ALTER TABLE budget_alerts ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation_budget_alerts ON budget_alerts;
CREATE POLICY tenant_isolation_budget_alerts ON budget_alerts
    USING (tenant_id = current_setting('app.tenant_id', true)::VARCHAR);
