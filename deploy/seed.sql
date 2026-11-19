-- Development data: two hives with readings every 5 minutes for the last
-- 24 hours. Safe to run more than once.
--
-- PowerShell:
--   Get-Content deploy/seed.sql | docker compose -f deploy/compose.yaml exec -T postgres psql -U hive -d hive

INSERT INTO hives (id, name) VALUES
    ('hive-1', 'Garden, left'),
    ('hive-2', 'Garden, right')
ON CONFLICT (id) DO NOTHING;

-- hive-1 slowly gains weight and has a temperature sensor.
-- hive-2 slowly loses weight and has no temperature sensor.
WITH series AS (
    SELECT t, extract(epoch FROM t - (date_trunc('hour', now()) - interval '24 hours')) / 3600 AS hours
    FROM generate_series(
        date_trunc('hour', now()) - interval '24 hours',
        date_trunc('hour', now()),
        interval '5 minutes'
    ) AS t
)
INSERT INTO hive_readings (hive_id, observed_at, received_at, weight_kg, temperature_c)
SELECT h.id,
       s.t,
       s.t + interval '2 seconds',
       round((h.base + h.trend * s.hours + (random() - 0.5) * 0.1)::numeric, 2),
       CASE WHEN h.has_temp THEN round((34.5 + (random() - 0.5))::numeric, 1) END
FROM series s
CROSS JOIN (VALUES
    ('hive-1', 42.0,  0.04, true),
    ('hive-2', 38.0, -0.02, false)
) AS h(id, base, trend, has_temp)
ON CONFLICT (hive_id, observed_at) DO NOTHING;
