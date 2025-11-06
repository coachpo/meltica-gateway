ALTER TABLE strategy_instances
    ADD COLUMN instance_id TEXT;

ALTER TABLE strategy_instances
    ALTER COLUMN instance_id SET NOT NULL;

ALTER TABLE strategy_instances
    ADD CONSTRAINT strategy_instances_instance_id_key UNIQUE (instance_id);
