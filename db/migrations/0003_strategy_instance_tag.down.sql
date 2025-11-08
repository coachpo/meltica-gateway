ALTER INDEX strategy_instances_identifier_tag_idx
    RENAME TO strategy_instances_identifier_version_idx;

ALTER TABLE strategy_instances
    RENAME COLUMN tag TO version;
