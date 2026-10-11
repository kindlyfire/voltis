-- A constant default adds the column without rewriting the table.
ALTER TABLE content ADD COLUMN special BOOLEAN NOT NULL DEFAULT false;
