ALTER TABLE strategy_instances
    ADD COLUMN instance_id TEXT;

UPDATE strategy_instances
SET instance_id = gen_random_uuid()::TEXT
WHERE instance_id IS NULL;

ALTER TABLE strategy_instances
    ALTER COLUMN instance_id SET NOT NULL;

ALTER TABLE strategy_instances
    ADD CONSTRAINT strategy_instances_instance_id_key UNIQUE (instance_id);
