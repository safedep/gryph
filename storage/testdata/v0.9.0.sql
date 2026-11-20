-- The schema and two rows of an audit database that Gryph v0.9.0 created.
-- storage/upgrade_test.go migrates it with the current schema.
CREATE TABLE `aarm_context_actions` (`id` uuid NOT NULL, `session_id` uuid NOT NULL, `event_id` uuid NULL, `timestamp` datetime NOT NULL, `action_type` text NOT NULL, `tool` text NULL, `agent` text NULL, `project` text NULL, `working_dir` text NULL, `result_status` text NOT NULL DEFAULT ('pending'), `duration_ms` integer NULL, `error_message` text NULL, `data_classifications` json NULL, `injection_score` real NULL, `sequence` integer NULL, `prev_hash` blob NULL, `hash` blob NULL, PRIMARY KEY (`id`));
CREATE TABLE `aarm_context_states` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `session_id` uuid NOT NULL, `first_seen_at` datetime NOT NULL, `last_action_at` datetime NOT NULL, `total_actions` integer NOT NULL DEFAULT (0), `files_read` integer NOT NULL DEFAULT (0), `files_written` integer NOT NULL DEFAULT (0), `commands_executed` integer NOT NULL DEFAULT (0), `network_requests` integer NOT NULL DEFAULT (0), `errors` integer NOT NULL DEFAULT (0), `tools_used` json NULL, `classifications_seen` json NULL, `entities_seen` json NULL, `semantic_drift` real NOT NULL DEFAULT (0));
CREATE TABLE `aarm_deferred_actions` (`id` uuid NOT NULL, `session_id` uuid NOT NULL, `receipt_sequence` integer NOT NULL, `action_id` uuid NOT NULL, `deferred_at` datetime NOT NULL, `expires_at` datetime NOT NULL, `reason` text NOT NULL, `status` text NOT NULL DEFAULT ('pending'), `resolved_at` datetime NULL, `resolver` text NULL, `resolution_note` text NULL, PRIMARY KEY (`id`));
CREATE TABLE `aarm_receipts` (`id` uuid NOT NULL, `session_id` uuid NOT NULL, `action_id` uuid NULL, `event_id` uuid NULL, `recorded_at` datetime NOT NULL, `sequence` integer NOT NULL, `agent` text NULL, `tool` text NULL, `action_type` text NOT NULL, `project` text NULL, `decision` text NOT NULL, `matched_rule_ids` json NULL, `severity` text NULL, `message` text NULL, `result_status` text NOT NULL DEFAULT ('pending'), `duration_ms` integer NULL, `error_message` text NULL, `snapshot` json NULL, `action_payload` json NULL, `prev_hash` blob NULL, `hash` blob NOT NULL, `subagent_id` text NULL, `subagent_type` text NULL, `policy_hash` blob NULL, `signature` blob NULL, `signer_key_id` text NULL, `defer_reason` text NULL, `deferral_of_sequence` integer NULL, `human_principal` text NULL, `service_identity` text NULL, `role_scope` text NULL, PRIMARY KEY (`id`));
CREATE TABLE `audit_events` (`id` uuid NOT NULL, `sequence` integer NOT NULL, `timestamp` datetime NOT NULL, `duration_ms` integer NULL, `agent_name` text NOT NULL, `agent_version` text NULL, `working_directory` text NULL, `action_type` text NOT NULL, `tool_name` text NULL, `result_status` text NOT NULL DEFAULT ('success'), `error_message` text NULL, `payload` json NULL, `diff_content` text NULL, `raw_event` json NULL, `conversation_context` text NULL, `is_sensitive` bool NOT NULL DEFAULT (false), `subagent_id` text NULL, `subagent_type` text NULL, `session_id` uuid NOT NULL, PRIMARY KEY (`id`), CONSTRAINT `audit_events_sessions_events` FOREIGN KEY (`session_id`) REFERENCES `sessions` (`id`) ON DELETE NO ACTION);
CREATE TABLE `audit_stream_cursors` (`id` text NOT NULL, `last_synced_at` datetime NOT NULL, `last_id` text NULL, PRIMARY KEY (`id`));
CREATE TABLE `event_stream_cursors` (`id` text NOT NULL, `last_synced_at` datetime NOT NULL, `last_id` text NULL, PRIMARY KEY (`id`));
CREATE VIRTUAL TABLE events_fts USING fts5(
    event_id UNINDEXED,
    session_id UNINDEXED,
    searchable_text,
    tokenize='porter unicode61'
);
CREATE TABLE events_fts_meta (
    key TEXT PRIMARY KEY,
    value TEXT
);
CREATE TABLE `self_audits` (`id` uuid NOT NULL, `timestamp` datetime NOT NULL, `action` text NOT NULL, `agent_name` text NULL, `details` json NULL, `result` text NOT NULL DEFAULT ('success'), `error_message` text NULL, `tool_version` text NOT NULL, PRIMARY KEY (`id`));
CREATE TABLE `sessions` (`id` uuid NOT NULL, `agent_session_id` text NULL, `agent_name` text NOT NULL, `agent_version` text NULL, `started_at` datetime NOT NULL, `ended_at` datetime NULL, `working_directory` text NULL, `project_name` text NULL, `total_actions` integer NOT NULL DEFAULT (0), `files_read` integer NOT NULL DEFAULT (0), `files_written` integer NOT NULL DEFAULT (0), `commands_executed` integer NOT NULL DEFAULT (0), `errors` integer NOT NULL DEFAULT (0), `sensitive_actions` integer NOT NULL DEFAULT (0), `blocked_actions` integer NOT NULL DEFAULT (0), `transcript_path` text NULL, `input_tokens` integer NOT NULL DEFAULT (0), `output_tokens` integer NOT NULL DEFAULT (0), `cache_read_tokens` integer NOT NULL DEFAULT (0), `cache_write_tokens` integer NOT NULL DEFAULT (0), `estimated_cost_usd` real NOT NULL DEFAULT (0), `model_usage` json NULL, `cost_source` text NULL, `cost_computed_at` datetime NULL, PRIMARY KEY (`id`));
CREATE UNIQUE INDEX `aarm_context_states_session_id_key` ON `aarm_context_states` (`session_id`);
CREATE INDEX `aarmcontextaction_session_id_action_type` ON `aarm_context_actions` (`session_id`, `action_type`);
CREATE UNIQUE INDEX `aarmcontextaction_session_id_sequence` ON `aarm_context_actions` (`session_id`, `sequence`);
CREATE INDEX `aarmcontextaction_session_id_timestamp` ON `aarm_context_actions` (`session_id`, `timestamp`);
CREATE INDEX `aarmcontextaction_timestamp` ON `aarm_context_actions` (`timestamp`);
CREATE INDEX `aarmcontextstate_last_action_at` ON `aarm_context_states` (`last_action_at`);
CREATE INDEX `aarmdeferredaction_expires_at` ON `aarm_deferred_actions` (`expires_at`);
CREATE INDEX `aarmdeferredaction_session_id_deferred_at` ON `aarm_deferred_actions` (`session_id`, `deferred_at`);
CREATE UNIQUE INDEX `aarmdeferredaction_session_id_receipt_sequence` ON `aarm_deferred_actions` (`session_id`, `receipt_sequence`);
CREATE INDEX `aarmdeferredaction_status` ON `aarm_deferred_actions` (`status`);
CREATE INDEX `aarmreceipt_action_id` ON `aarm_receipts` (`action_id`);
CREATE INDEX `aarmreceipt_decision` ON `aarm_receipts` (`decision`);
CREATE INDEX `aarmreceipt_recorded_at` ON `aarm_receipts` (`recorded_at`);
CREATE INDEX `aarmreceipt_session_id_deferral_of_sequence` ON `aarm_receipts` (`session_id`, `deferral_of_sequence`) WHERE deferral_of_sequence IS NOT NULL;
CREATE INDEX `aarmreceipt_session_id_recorded_at` ON `aarm_receipts` (`session_id`, `recorded_at`);
CREATE UNIQUE INDEX `aarmreceipt_session_id_sequence` ON `aarm_receipts` (`session_id`, `sequence`);
CREATE INDEX `auditevent_action_type` ON `audit_events` (`action_type`);
CREATE INDEX `auditevent_agent_name` ON `audit_events` (`agent_name`);
CREATE INDEX `auditevent_result_status` ON `audit_events` (`result_status`);
CREATE INDEX `auditevent_session_id` ON `audit_events` (`session_id`);
CREATE INDEX `auditevent_timestamp` ON `audit_events` (`timestamp`);
CREATE INDEX `selfaudit_action` ON `self_audits` (`action`);
CREATE INDEX `selfaudit_timestamp` ON `self_audits` (`timestamp`);
CREATE INDEX `session_agent_name` ON `sessions` (`agent_name`);
CREATE INDEX `session_agent_name_started_at` ON `sessions` (`agent_name`, `started_at`);
CREATE INDEX `session_started_at` ON `sessions` (`started_at`);
INSERT INTO `sessions` (`id`, `agent_session_id`, `agent_name`, `agent_version`, `started_at`, `ended_at`, `working_directory`, `project_name`, `total_actions`, `files_read`, `files_written`, `commands_executed`, `errors`, `sensitive_actions`, `blocked_actions`, `transcript_path`, `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens`, `estimated_cost_usd`, `model_usage`, `cost_source`, `cost_computed_at`) VALUES ('ee3a6f02-6946-5243-bde8-81c2bd576ebe', 's1', 'claude-code', NULL, '2026-11-20 10:57:13.248347498 +0000 UTC', NULL, '/tmp', 'tmp', 1, 0, 0, 1, 0, 0, 0, NULL, 0, 0, 0, 0, 0.0, NULL, NULL, NULL);
INSERT INTO `audit_events` (`id`, `sequence`, `timestamp`, `duration_ms`, `agent_name`, `agent_version`, `working_directory`, `action_type`, `tool_name`, `result_status`, `error_message`, `payload`, `diff_content`, `raw_event`, `conversation_context`, `is_sensitive`, `subagent_id`, `subagent_type`, `session_id`) VALUES ('b54ab515-2097-482b-908a-6f36a3ef1e39', 1, '2026-11-20 10:57:13.245676263 +0000 UTC', NULL, 'claude-code', NULL, '/tmp', 'command_exec', 'Bash', 'success', NULL, X'7b22636f6d6d616e64223a226c73222c22657869745f636f6465223a307d', NULL, NULL, NULL, 0, NULL, NULL, 'ee3a6f02-6946-5243-bde8-81c2bd576ebe');
