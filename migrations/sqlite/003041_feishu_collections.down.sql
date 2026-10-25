CREATE TEMP TABLE feishu_collection_rollback_guard(n INTEGER CHECK(n=0));
INSERT INTO feishu_collection_rollback_guard SELECT count(*) FROM feishu_collections;
INSERT INTO feishu_collection_rollback_guard SELECT count(*) FROM feishu_collection_pages;
DROP TABLE feishu_collection_pages;
DROP TABLE feishu_collections;
DROP TABLE feishu_collection_rollback_guard;
