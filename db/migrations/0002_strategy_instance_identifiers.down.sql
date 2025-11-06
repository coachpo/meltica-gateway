ALTER TABLE strategy_instances
    DROP CONSTRAINT IF EXISTS strategy_instances_instance_id_key;

ALTER TABLE strategy_instances
    DROP COLUMN IF EXISTS instance_id;
