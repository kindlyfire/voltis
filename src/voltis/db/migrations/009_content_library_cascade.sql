ALTER TABLE content
    DROP CONSTRAINT content_library_id_fkey,
    ADD CONSTRAINT content_library_id_fkey
        FOREIGN KEY (library_id) REFERENCES libraries(id) ON DELETE CASCADE;
