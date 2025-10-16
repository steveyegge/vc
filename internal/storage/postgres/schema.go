package postgres

const schema = `
-- Issue ID sequence
-- Used to generate unique sequential IDs for issues
CREATE SEQUENCE IF NOT EXISTS issue_id_seq START WITH 1;

-- Function to generate issue IDs with 'vc-' prefix
CREATE OR REPLACE FUNCTION next_issue_id() RETURNS TEXT AS $$
BEGIN
    RETURN 'vc-' || nextval('issue_id_seq')::TEXT;
END;
$$ LANGUAGE plpgsql;

-- Issues table
CREATE TABLE IF NOT EXISTS issues (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL CHECK(LENGTH(title) <= 500),
    description TEXT NOT NULL DEFAULT '',
    design TEXT NOT NULL DEFAULT '',
    acceptance_criteria TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',
    priority INTEGER NOT NULL DEFAULT 2 CHECK(priority >= 0 AND priority <= 4),
    issue_type TEXT NOT NULL DEFAULT 'task',
    assignee TEXT,
    estimated_minutes INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    closed_at TIMESTAMPTZ,
    approved_at TIMESTAMPTZ,
    approved_by TEXT
);

CREATE INDEX IF NOT EXISTS idx_issues_status ON issues(status);
CREATE INDEX IF NOT EXISTS idx_issues_priority ON issues(priority);
CREATE INDEX IF NOT EXISTS idx_issues_assignee ON issues(assignee);
CREATE INDEX IF NOT EXISTS idx_issues_created_at ON issues(created_at);

-- Dependencies table
CREATE TABLE IF NOT EXISTS dependencies (
    issue_id TEXT NOT NULL,
    depends_on_id TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'blocks',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    PRIMARY KEY (issue_id, depends_on_id),
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE,
    FOREIGN KEY (depends_on_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_dependencies_issue ON dependencies(issue_id);
CREATE INDEX IF NOT EXISTS idx_dependencies_depends_on ON dependencies(depends_on_id);

-- Labels table
CREATE TABLE IF NOT EXISTS labels (
    issue_id TEXT NOT NULL,
    label TEXT NOT NULL,
    PRIMARY KEY (issue_id, label),
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_labels_label ON labels(label);

-- Events table (audit trail)
CREATE TABLE IF NOT EXISTS events (
    id BIGSERIAL PRIMARY KEY,
    issue_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    actor TEXT NOT NULL,
    old_value TEXT,
    new_value TEXT,
    comment TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_events_issue ON events(issue_id);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at);

-- Ready work view
CREATE OR REPLACE VIEW ready_issues AS
SELECT i.*
FROM issues i
WHERE i.status = 'open'
  AND NOT EXISTS (
    SELECT 1 FROM dependencies d
    JOIN issues blocked ON d.depends_on_id = blocked.id
    WHERE d.issue_id = i.id
      AND d.type = 'blocks'
      AND blocked.status IN ('open', 'in_progress', 'blocked')
  );

-- Blocked issues view
CREATE OR REPLACE VIEW blocked_issues AS
SELECT
    i.*,
    COUNT(d.depends_on_id) as blocked_by_count
FROM issues i
JOIN dependencies d ON i.id = d.issue_id
JOIN issues blocker ON d.depends_on_id = blocker.id
WHERE i.status IN ('open', 'in_progress', 'blocked')
  AND d.type = 'blocks'
  AND blocker.status IN ('open', 'in_progress', 'blocked')
GROUP BY i.id, i.title, i.description, i.design, i.acceptance_criteria, i.notes,
         i.status, i.priority, i.issue_type, i.assignee, i.estimated_minutes,
         i.created_at, i.updated_at, i.closed_at, i.approved_at, i.approved_by;

-- Executor instances table
-- Tracks running executor instances for multi-executor coordination
CREATE TABLE IF NOT EXISTS executor_instances (
    instance_id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    pid INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'running' CHECK(status IN ('running', 'stopped')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_heartbeat TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    version TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_executor_instances_status ON executor_instances(status);
CREATE INDEX IF NOT EXISTS idx_executor_instances_heartbeat ON executor_instances(last_heartbeat);

-- Issue execution state table
-- Tracks the execution state of issues being processed by executors
-- Enables checkpoint/resume functionality and prevents double-claiming
CREATE TABLE IF NOT EXISTS issue_execution_state (
    issue_id TEXT PRIMARY KEY,
    executor_instance_id TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'claimed' CHECK(state IN ('claimed', 'assessing', 'executing', 'analyzing', 'gates', 'committing', 'completed')),
    checkpoint_data JSONB NOT NULL DEFAULT '{}',
    started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE,
    FOREIGN KEY (executor_instance_id) REFERENCES executor_instances(instance_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_execution_state_executor ON issue_execution_state(executor_instance_id);
CREATE INDEX IF NOT EXISTS idx_execution_state_state ON issue_execution_state(state);
CREATE INDEX IF NOT EXISTS idx_execution_state_updated ON issue_execution_state(updated_at);

-- Agent events table
-- Tracks events extracted from agent execution output (file changes, tests, git ops, errors, etc.)
-- AND executor-level events (issue claimed, assessment, agent lifecycle, quality gates, etc.)
CREATE TABLE IF NOT EXISTS agent_events (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK(type IN (
        -- Agent output events
        'file_modified', 'test_run', 'git_operation', 'build_output', 'lint_output', 'progress', 'error', 'watchdog_alert',
        -- Executor-level events
        'issue_claimed', 'assessment_started', 'assessment_completed',
        'agent_spawned', 'agent_completed',
        'results_processing_started', 'results_processing_completed',
        'analysis_started', 'analysis_completed',
        'quality_gates_started', 'quality_gates_completed', 'quality_gates_skipped'
    )),
    timestamp TIMESTAMPTZ NOT NULL,
    issue_id TEXT NOT NULL,
    executor_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    severity TEXT NOT NULL CHECK(severity IN ('info', 'warning', 'error', 'critical')),
    message TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}',
    source_line INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_agent_events_issue ON agent_events(issue_id);
CREATE INDEX IF NOT EXISTS idx_agent_events_type ON agent_events(type);
CREATE INDEX IF NOT EXISTS idx_agent_events_severity ON agent_events(severity);
CREATE INDEX IF NOT EXISTS idx_agent_events_timestamp ON agent_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_agent_events_executor ON agent_events(executor_id);
`
