-- +goose Up
CREATE TABLE hives (
    id         text        PRIMARY KEY,
    name       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- One row per scale reading. The primary key makes a repeated
-- (hive, sensor time) pair impossible, so a message delivered twice
-- over MQTT cannot create a duplicate. It also serves as the index for
-- "readings of one hive between two times".
CREATE TABLE hive_readings (
    hive_id       text             NOT NULL REFERENCES hives (id),
    observed_at   timestamptz      NOT NULL, -- sensor time
    received_at   timestamptz      NOT NULL DEFAULT now(), -- server time
    weight_kg     double precision NOT NULL,
    temperature_c double precision, -- NULL when the scale has no temperature sensor
    PRIMARY KEY (hive_id, observed_at)
);

-- +goose Down
DROP TABLE hive_readings;
DROP TABLE hives;
