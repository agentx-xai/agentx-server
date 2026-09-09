-- +goose Up
ALTER TABLE device_packages DROP CONSTRAINT device_packages_device_id_fkey;
ALTER TABLE devices DROP CONSTRAINT devices_pkey;
ALTER TABLE devices ALTER COLUMN id TYPE text USING id::text;
ALTER TABLE devices ADD PRIMARY KEY (workspace_id, id);

ALTER TABLE device_packages ALTER COLUMN device_id TYPE text USING device_id::text;
ALTER TABLE device_packages ADD COLUMN workspace_id uuid;
UPDATE device_packages AS dp
SET workspace_id = d.workspace_id
FROM devices AS d
WHERE d.id = dp.device_id;
ALTER TABLE device_packages ALTER COLUMN workspace_id SET NOT NULL;
ALTER TABLE device_packages DROP CONSTRAINT device_packages_pkey;
ALTER TABLE device_packages ADD PRIMARY KEY (workspace_id, device_id, name);
ALTER TABLE device_packages ADD CONSTRAINT device_packages_device_fkey
    FOREIGN KEY (workspace_id, device_id) REFERENCES devices(workspace_id, id) ON DELETE CASCADE;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM devices
        WHERE id !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    ) THEN
        RAISE EXCEPTION 'cannot roll back: non-UUID device IDs exist';
    END IF;
    IF EXISTS (
        SELECT id FROM devices GROUP BY id HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot roll back: device IDs are duplicated across workspaces';
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE device_packages DROP CONSTRAINT device_packages_device_fkey;
ALTER TABLE device_packages DROP CONSTRAINT device_packages_pkey;
ALTER TABLE device_packages ALTER COLUMN device_id TYPE uuid USING device_id::uuid;
ALTER TABLE device_packages ADD PRIMARY KEY (device_id, name);
ALTER TABLE device_packages DROP COLUMN workspace_id;

ALTER TABLE devices DROP CONSTRAINT devices_pkey;
ALTER TABLE devices ALTER COLUMN id TYPE uuid USING id::uuid;
ALTER TABLE devices ADD PRIMARY KEY (id);
ALTER TABLE device_packages ADD CONSTRAINT device_packages_device_id_fkey
    FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE;
