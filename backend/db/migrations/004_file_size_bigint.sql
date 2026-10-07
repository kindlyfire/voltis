-- Files and series totals above 2 GiB overflow INTEGER.
ALTER TABLE content ALTER COLUMN file_size TYPE BIGINT;
