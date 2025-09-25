ALTER TABLE roles
    DROP COLUMN IF EXISTS skills,
    DROP COLUMN IF EXISTS languages,
    DROP COLUMN IF EXISTS background,
    DROP COLUMN IF EXISTS personality;
