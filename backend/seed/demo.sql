-- Synthetic demo data. Applied at start when DEMO_SEED=true; safe to run repeatedly.
-- Names match the frontend prototype (frontend/src/domain.ts, scenarios.ts).

INSERT INTO users(id, display_name) VALUES
  ('u-sasha',  'Саша Соколов'),
  ('u-masha',  'Маша Петрова'),
  ('u-ilya',   'Илья Смирнов'),
  ('u-dasha',  'Даша Морозова'),
  ('u-alex',   'Алексей Романов'),
  ('u-anna',   'Анна Петрова'),
  -- second team, for access-isolation checks
  ('u-kirill', 'Кирилл Орлов'),
  ('u-oleg',   'Олег Кузнецов'),
  ('u-irina',  'Ирина Волкова')
ON CONFLICT (id) DO NOTHING;

INSERT INTO assignments(id, intern_id, buddy_id, lead_id, team_id) VALUES
  ('assignment-sasha',  'u-sasha',  'u-dasha', 'u-alex',  'platform'),
  ('assignment-masha',  'u-masha',  'u-dasha', 'u-alex',  'platform'),
  ('assignment-ilya',   'u-ilya',   'u-dasha', 'u-alex',  'platform'),
  ('assignment-kirill', 'u-kirill', 'u-oleg',  'u-irina', 'mobile')
ON CONFLICT (id) DO NOTHING;
