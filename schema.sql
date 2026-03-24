1.  Create a "university" database
-- -----------------------------------------------------------------------------


-- -----------------------------------------------------------------------------
-- 2.  Table definition
-- -----------------------------------------------------------------------------

DROP TABLE IF EXISTS students;

CREATE TABLE students (
  
    id        BIGSERIAL    PRIMARY KEY,
    name      VARCHAR(100) NOT NULL,
    programme TEXT         NOT NULL,
    year      SMALLINT     NOT NULL CHECK (year BETWEEN 1 AND 4),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- An index on name speeds up future searches and demonstrates index creation.
CREATE INDEX idx_students_name ON students (name);


-- -----------------------------------------------------------------------------
-- 3.  Seed data
--     These rows match the hard-coded slice in the original demo so existing
--     curl tests still produce familiar results.
-- -----------------------------------------------------------------------------

INSERT INTO students (name, programme, year) VALUES
    ('Eve Castillo',   'BSc Computer Science',    2),
    ('Marco Tillett',  'BSc Computer Science',    3),
    ('Aisha Gentle',   'BSc Information Systems', 1),
    ('Raj Palacio',    'BSc Computer Science',    4);


-- -----------------------------------------------------------------------------
-- 4.  Verify
-- -----------------------------------------------------------------------------

SELECT * FROM students;