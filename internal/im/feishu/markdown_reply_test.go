package feishu

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/im"
)

func TestMarkdownReplyPreservesFormattingThroughThreadFallback(t *testing.T) {
	useTestHTTPClient(t)
	fake := &fakeOpenPlatform{replyCode: 230071}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	a, err := NewAdapter(testRegion(srv.URL), "app", "secret", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	answer := "# ELOG 操作单\n\n**风险等级**：中\n\n| 项 | 命令 |\n|---|---|\n| 验证 | `docker ps` |\n\n```bash\nprintf '%s\\n' '$HOME'\n```\n\n[来源](https://example.com/doc)"
	err = a.SendReply(context.Background(), &im.IncomingMessage{MessageID: "om_test", ChatType: im.ChatTypeGroup, ChatID: "oc_test"}, &im.ReplyMessage{Content: answer, IsFinal: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []map[string]any{fake.replyBody, fake.sendBody} {
		if payload["msg_type"] != "post" {
			t.Fatalf("Markdown degraded: %#v", payload)
		}
		var content struct {
			Chinese struct {
				Content [][]struct{ Tag, Text string }
			} `json:"zh_cn"`
		}
		if err := json.Unmarshal([]byte(payload["content"].(string)), &content); err != nil {
			t.Fatal(err)
		}
		if len(content.Chinese.Content) != 1 || len(content.Chinese.Content[0]) != 1 {
			t.Fatal("unexpected post shape")
		}
		node := content.Chinese.Content[0][0]
		if node.Tag != "md" || node.Text != answer {
			t.Fatalf("Markdown changed: %#v", node)
		}
	}
}

func TestMarkdownReplyLongFencesAndTables(t *testing.T) {
	for _, marker := range []string{"```", "~~~~"} {
		answer := "# Commands\n\n" + marker + "bash\n" + strings.Repeat("echo 中文参数\n", 600) + marker + "\n"
		parts := splitMarkdownReply(answer)
		commands := 0
		for _, part := range parts {
			if len(part) > textReplyChunkBytes || !utf8.ValidString(part) {
				t.Fatal("invalid message boundary")
			}
			if strings.Count(part, marker)%2 != 0 {
				t.Fatalf("unclosed fence: %q", part)
			}
			commands += strings.Count(part, "echo 中文参数\n")
		}
		if commands != 600 {
			t.Fatalf("commands lost or duplicated: %d", commands)
		}
	}
	header := "| 项目 | 结果 |\n|---|---|\n"
	parts := splitMarkdownReply(header + strings.Repeat("| 验证 | 正常 |\n", 400))
	rows := 0
	for _, part := range parts {
		if !strings.HasPrefix(part, header) || len(part) > textReplyChunkBytes {
			t.Fatal("table header/budget lost")
		}
		rows += strings.Count(part, "| 验证 | 正常 |\n")
	}
	if rows != 400 {
		t.Fatalf("table rows lost: %d", rows)
	}
}

func TestMarkdownReplyLongLineMakesProgress(t *testing.T) {
	for _, text := range []string{strings.Repeat("中文", 10000), strings.Repeat("\x00", 10000), "```" + strings.Repeat("x", 5000) + "\n"} {
		parts := splitMarkdownReply(text)
		if strings.Join(parts, "") != text {
			t.Fatal("long line changed")
		}
		for _, part := range parts {
			if len(part) > textReplyChunkBytes || !utf8.ValidString(part) {
				t.Fatal("invalid chunk")
			}
		}
	}
}
