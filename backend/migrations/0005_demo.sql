-- Demo stand settings (DEMO_MODE only): the moved "today" survives a restart of the API.
CREATE TABLE demo_settings (
  key    text PRIMARY KEY,
  value  text NOT NULL
);
