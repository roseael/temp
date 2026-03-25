CREATE TABLE IF NOT EXISTS courses (
    code text PRIMARY KEY,
    title text NOT NULL,
    credits integer NOT NULL,
    instructors text[],
    enrolled integer NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for performance on course titles
CREATE INDEX IF NOT EXISTS courses_title_idx ON courses (title);

INSERT INTO course (code, title, credits) VALUES
    ('CMPS2242', 'Systems Programming & Computer Organization', 3),
    ('CMPS2212', 'GUI Programming', 3),
    ('CMPS2232', 'Object Oriented Programming', 3),
    ('CMPS1171', 'Introduction to Databases', 3);
