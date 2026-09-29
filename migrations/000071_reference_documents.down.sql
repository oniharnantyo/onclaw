-- Reverse 000071 (add-reference-documents): drop the section index, the
-- visibility joins, and the document registry, then remove the promote
-- permission from built-in roles. Documents stored in the blob store remain
-- orphaned but harmless — the attachment-blob precedent.

UPDATE roles
SET permissions = array_remove(permissions, 'reference_documents.promote')
WHERE built_in = true;

DROP TABLE IF EXISTS document_sections;
DROP TABLE IF EXISTS reference_document_channels;
DROP TABLE IF EXISTS reference_document_agents;
DROP TABLE IF EXISTS reference_documents;
