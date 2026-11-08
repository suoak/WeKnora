import assert from 'node:assert/strict';
import test from 'node:test';

import { buildListKnowledgeFilesQuery } from './knowledgeFileListQuery';

test('知识文件列表请求会传递排序字段和方向', () => {
  const query = new URLSearchParams(buildListKnowledgeFilesQuery({
    page: 2,
    page_size: 35,
    sort_by: 'file_name',
    sort_order: 'asc',
    folder_path: '',
  }));

  assert.equal(query.get('page'), '2');
  assert.equal(query.get('page_size'), '35');
  assert.equal(query.get('sort_by'), 'file_name');
  assert.equal(query.get('sort_order'), 'asc');
  assert.equal(query.get('folder_path'), '');
});


test('Chinese, root and recursive folder queries preserve parameter presence and round-trip paths', () => {
  for (const path of ['拜访纪要', '洞察指南/子目录', '']) {
    const encoded = buildListKnowledgeFilesQuery({ page: 2, page_size: 10, folder_path: path, folder_recursive: true });
    const query = new URLSearchParams(encoded);
    assert.equal(query.get('folder_path'), path);
    assert.equal(query.get('folder_recursive'), 'true');
    assert.equal(query.get('page'), '2');
  }
  assert.equal(new URLSearchParams(buildListKnowledgeFilesQuery({ page: 1, page_size: 35 })).has('folder_path'), false);
});
