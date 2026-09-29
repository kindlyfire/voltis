ALTER TABLE content ADD COLUMN word_count INTEGER, ADD COLUMN page_count INTEGER;

UPDATE content SET page_count = jsonb_array_length(file_data->'pages')
WHERE type = 'comic' AND jsonb_typeof(file_data->'pages') = 'array';

-- file_data's word map has no spine linearity; a size mismatch makes the next scan reparse books.
UPDATE content SET file_size = NULL WHERE type = 'book';
