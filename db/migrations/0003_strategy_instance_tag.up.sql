ALTER TABLE strategy_instances
    RENAME COLUMN version TO tag;

ALTER INDEX strategy_instances_identifier_version_idx
    RENAME TO strategy_instances_identifier_tag_idx;
