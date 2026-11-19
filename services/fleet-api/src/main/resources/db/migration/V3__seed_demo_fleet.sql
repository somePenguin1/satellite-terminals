-- Deterministic demo data: 7 plans, 800 customers and 5,000 terminals with configs.
-- setseed() makes random() repeatable, so every environment gets the same fleet.
SELECT setseed(0.42);

INSERT INTO service_plans (code, name, family, tier, downlink_mbps, uplink_mbps, monthly_price_cents)
VALUES ('RES-STD', 'Residential Standard', 'residential', 1, 100, 10, 9900),
       ('RES-PRI', 'Residential Priority', 'residential', 2, 220, 25, 16500),
       ('BIZ-STD', 'Business Standard', 'business', 1, 200, 20, 25000),
       ('BIZ-PRO', 'Business Pro', 'business', 2, 350, 40, 50000),
       ('ENT-MAX', 'Enterprise Max', 'business', 3, 500, 75, 120000),
       ('MAR-CST', 'Maritime Coastal', 'maritime', 1, 100, 15, 75000),
       ('MAR-GLB', 'Maritime Global', 'maritime', 2, 300, 40, 200000);

WITH words AS (
    SELECT ARRAY ['Northwind','Bluewater','Summit','Prairie','Harbor','Granite','Cedar','Aurora','Meridian','Redwood',
                  'Silverline','Ironclad','Pioneer','Lakeshore','Highland','Sunrise','Polaris','Atlas','Evergreen','Coastal'] AS prefix,
           ARRAY ['Logistics','Farms','Health','Energy','Mining','Schools','Lodge','Outfitters','Telecom','Construction'] AS biz,
           ARRAY ['Okafor','Lindqvist','Haddad','Moreau','Tanaka','Silva','Kowalski','Nguyen','Mensah','Fitzgerald',
                  'Rossi','Patel','Novak','Herrera','Johansson','Adeyemi','Kim','Dubois','Murphy','Castillo'] AS surnames,
           ARRAY ['North America','South America','Europe','Africa','Asia Pacific','Oceania'] AS regions,
           ARRAY ['Priya Raman','Marcus Webb','Elena Petrova','Jonah Mills','Aisha Bello','Tom Garrity','Lucia Ortega','Ken Ito'] AS managers
),
     c AS (
         SELECT n,
                CASE
                    WHEN n <= 400 THEN 'residential'
                    WHEN n <= 600 THEN 'business'
                    WHEN n <= 680 THEN 'maritime'
                    WHEN n <= 760 THEN 'enterprise'
                    ELSE 'government'
                    END AS segment
         FROM generate_series(1, 800) n
     )
INSERT INTO customers (id, name, segment, region, account_manager, contact_email, created_at)
SELECT c.n,
       CASE c.segment
           WHEN 'residential' THEN w.surnames[1 + c.n % 20] || ' Household #' || c.n
           WHEN 'business' THEN w.prefix[1 + c.n % 20] || ' ' || w.biz[1 + (c.n / 20) % 10]
           WHEN 'maritime' THEN w.prefix[1 + c.n % 20] || ' Shipping Co.'
           WHEN 'enterprise' THEN w.prefix[1 + c.n % 20] || ' ' || w.biz[1 + c.n % 10] || ' Group'
           ELSE 'Regional Authority ' || w.surnames[1 + c.n % 20] || '-' || c.n
           END,
       c.segment,
       CASE WHEN c.segment = 'maritime' THEN 'Maritime' ELSE w.regions[1 + (c.n * 7) % 6] END,
       w.managers[1 + c.n % 8],
       'ops+' || c.n || '@example.com',
       now() - (random() * interval '1000 days')
FROM c,
     words w;

SELECT setval(pg_get_serial_sequence('customers', 'id'), (SELECT max(id) FROM customers));

-- Terminals cluster around rural towns (where satellite broadband actually sells) and,
-- for maritime customers, along shipping lanes.
CREATE TEMPORARY TABLE seed_anchors (region text, lat double precision, lon double precision) ON COMMIT DROP;
INSERT INTO seed_anchors VALUES
    ('North America', 39.7, -105.0), ('North America', 61.2, -149.9), ('North America', 45.8, -108.5),
    ('North America', 43.6, -116.2), ('North America', 47.0, -110.0), ('North America', 52.1, -106.7),
    ('North America', 62.4, -114.4), ('North America', 30.3, -97.7), ('North America', 38.5, -98.0),
    ('North America', 45.2, -69.0), ('North America', 47.0, -71.2), ('North America', 35.1, -106.6),
    ('North America', 37.8, -81.2), ('North America', 54.5, -123.0),
    ('South America', -3.1, -60.0), ('South America', -16.5, -68.1), ('South America', -13.5, -72.0),
    ('South America', -24.8, -65.4), ('South America', -41.1, -71.3), ('South America', -15.6, -56.1),
    ('South America', 4.7, -74.1), ('South America', -8.1, -36.0), ('South America', -32.9, -56.2),
    ('South America', -23.6, -69.8),
    ('Europe', 69.6, 18.9), ('Europe', 57.5, -4.2), ('Europe', 53.3, -8.6), ('Europe', 37.4, -5.0),
    ('Europe', 46.6, 10.5), ('Europe', 43.8, 16.8), ('Europe', 67.0, 26.0), ('Europe', 35.2, 24.9),
    ('Europe', 48.5, 11.5), ('Europe', 48.1, -2.8), ('Europe', 62.5, 15.0),
    ('Africa', -1.3, 36.8), ('Africa', -3.4, 36.7), ('Africa', 7.5, 4.0), ('Africa', 6.7, -1.6),
    ('Africa', -22.6, 17.1), ('Africa', -15.4, 28.3), ('Africa', 9.0, 38.7), ('Africa', -4.3, 15.3),
    ('Africa', 14.7, -16.4), ('Africa', -24.5, 31.8), ('Africa', 12.6, -8.0),
    ('Asia Pacific', 7.2, 125.4), ('Asia Pacific', -2.0, 120.5), ('Asia Pacific', 1.5, 114.0),
    ('Asia Pacific', 43.1, 142.4), ('Asia Pacific', 34.2, 77.6), ('Asia Pacific', 47.9, 106.9),
    ('Asia Pacific', 16.4, 120.6), ('Asia Pacific', 26.1, 91.7), ('Asia Pacific', 19.9, 102.1),
    ('Asia Pacific', -0.9, 101.0),
    ('Oceania', -23.7, 133.9), ('Oceania', -18.5, 123.0), ('Oceania', -17.5, 145.0),
    ('Oceania', -42.0, 146.5), ('Oceania', -43.5, 171.8), ('Oceania', -17.8, 178.0),
    ('Oceania', -6.5, 145.0), ('Oceania', -32.2, 148.6), ('Oceania', -30.7, 121.5), ('Oceania', -13.5, 131.5),
    ('Maritime', 45.0, -40.0), ('Maritime', 35.0, -55.0), ('Maritime', 10.0, -30.0),
    ('Maritime', -10.0, 75.0), ('Maritime', 5.0, 65.0), ('Maritime', 20.0, -150.0),
    ('Maritime', 32.0, -170.0), ('Maritime', 0.0, -120.0), ('Maritime', 13.0, 55.0),
    ('Maritime', 6.0, 90.0), ('Maritime', -30.0, -15.0), ('Maritime', -38.0, 160.0), ('Maritime', 57.0, 3.0);

-- Expand each customer into N terminals, then number them SAT-00001..SAT-05000.
WITH anchors AS (
    SELECT region, lat, lon,
           (row_number() OVER (PARTITION BY region ORDER BY lat, lon) - 1)::int AS idx,
           count(*) OVER (PARTITION BY region) AS cnt
    FROM seed_anchors
),
     slots AS (
         SELECT cu.id AS customer_id,
                cu.segment,
                cu.region,
                row_number() OVER (ORDER BY cu.id, g) AS n
         FROM customers cu
                  CROSS JOIN LATERAL generate_series(1, CASE cu.segment
                                                             WHEN 'residential' THEN 1
                                                             WHEN 'business' THEN 2 + cu.id % 7
                                                             WHEN 'maritime' THEN 6 + cu.id % 15
                                                             WHEN 'enterprise' THEN 12 + cu.id % 30
                                                             ELSE 5 + cu.id % 20 END) g
     ),
     placed AS (
         SELECT s.*,
                random() AS r1,
                random() AS r2,
                random() AS r3,
                random() AS r4,
                random() AS r5
         FROM slots s
         WHERE s.n <= 5000
     ),
     located AS (
         SELECT p.*,
                a.lat + (p.r1 - 0.5) * CASE WHEN p.region = 'Maritime' THEN 10 ELSE 2.6 END AS raw_lat,
                a.lon + (p.r2 - 0.5) * CASE WHEN p.region = 'Maritime' THEN 20 ELSE 3.6 END AS raw_lon
         FROM placed p
                  JOIN anchors a ON a.region = p.region AND a.idx = floor(p.r5 * a.cnt)::int
     )
INSERT INTO terminals (id, serial_number, customer_id, plan_code, model, status, region,
                       latitude, longitude, activated_at, created_at)
SELECT format('SAT-%s', lpad(l.n::text, 5, '0')),
       format('UT%s-%s', to_char(1 + l.n % 4, 'FM0'), upper(substr(md5(l.n::text), 1, 10))),
       l.customer_id,
       CASE l.segment
           WHEN 'residential' THEN CASE WHEN l.r4 < 0.7 THEN 'RES-STD' ELSE 'RES-PRI' END
           WHEN 'maritime' THEN CASE WHEN l.r4 < 0.55 THEN 'MAR-CST' ELSE 'MAR-GLB' END
           WHEN 'business' THEN CASE WHEN l.r4 < 0.6 THEN 'BIZ-STD' ELSE 'BIZ-PRO' END
           ELSE CASE WHEN l.r4 < 0.45 THEN 'BIZ-PRO' ELSE 'ENT-MAX' END
           END,
       CASE l.segment
           WHEN 'residential' THEN 'Residential Kit G3'
           WHEN 'maritime' THEN 'Maritime Stabilized Ku'
           ELSE CASE WHEN l.r4 < 0.5 THEN 'Flat Panel Pro G2' ELSE 'Enterprise Ka Dish' END
           END,
       CASE
           WHEN l.n % 25 = 0 THEN 'provisioned'
           WHEN l.n % 97 = 0 THEN 'suspended'
           ELSE 'active'
           END,
       l.region,
       greatest(-85, least(85, l.raw_lat)),
       (((l.raw_lon + 540)::numeric % 360) - 180)::double precision,
       CASE WHEN l.n % 25 = 0 THEN NULL ELSE now() - (l.r3 * interval '900 days') END,
       now() - (l.r3 * interval '900 days') - interval '7 days'
FROM located l;

SELECT setval('terminal_number_seq', (SELECT count(*) FROM terminals));

-- Maritime terminals report less often (satellite airtime is costlier at sea);
-- enterprise sites get detailed telemetry and priority QoS.
INSERT INTO terminal_configs (terminal_id, report_interval_ms, telemetry_level, band, qos_profile, firmware_channel,
                              updated_by)
SELECT t.id,
       CASE WHEN c.segment = 'maritime' THEN 10000 ELSE 5000 END,
       CASE WHEN c.segment IN ('enterprise', 'government') THEN 'detailed' ELSE 'basic' END,
       CASE WHEN t.model = 'Enterprise Ka Dish' THEN 'Ka' ELSE 'Ku' END,
       CASE
           WHEN t.plan_code IN ('ENT-MAX', 'MAR-GLB') THEN 'business-critical'
           WHEN t.plan_code IN ('RES-PRI', 'BIZ-PRO') THEN 'priority'
           ELSE 'standard' END,
       CASE WHEN substr(t.id, 9, 1) = '7' THEN 'beta' ELSE 'stable' END,
       'seed'
FROM terminals t
         JOIN customers c ON c.id = t.customer_id;
