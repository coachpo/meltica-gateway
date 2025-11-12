DROP INDEX IF EXISTS strategy_instances_identifier_tag_idx;

CREATE UNIQUE INDEX strategy_instances_identifier_tag_idx
    ON strategy_instances (strategy_identifier, tag);
