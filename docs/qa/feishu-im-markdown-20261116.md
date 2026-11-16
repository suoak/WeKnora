# Feishu IM Markdown replies

Non-streaming replies previously used `msg_type=text`. A CardKit streaming
failure therefore left headings, bold markers, tables and fenced commands as
literal source text in the user's chat.

Send native `post` messages with a standalone `md` node under `zh_cn.content`.
Keep both the reply-to-original-message API and the existing group/thread
fallback using the same rich payload. This route uses the existing IM message
API, without a CardKit call. Existing streaming cards are unchanged.

The conservative 2 KiB source budget remains. Splitting preserves UTF-8 and
normal lines, carries code fences and repeats table headers. Oversized single
lines can still span messages; copy unusually long commands from their original
source. Some rendering styles require a current Feishu client.

Validation covers mixed headings/emphasis/table/code/source links, group reply
fallback, long code fences and tables, lossless plain text and UTF-8 boundaries.
Production visual acceptance requires a new real IM reply after deployment;
mocked API payload checks alone do not prove client rendering.

Reference: [Feishu message content contract](https://open.feishu.cn/document/server-docs/im-v1/message-content-description/create_json?lang=zh-CN).
