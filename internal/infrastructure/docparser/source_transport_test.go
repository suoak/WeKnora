package docparser

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/docreader/proto"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type sourceTransportServer struct {
	proto.UnimplementedDocReaderServer
	unary bool
}

func sourceTransportBlocks() []*proto.SourceBlock {
	return []*proto.SourceBlock{
		{Start: 0, End: 4, LocatorJson: `{"type":"sheet","sheet":"明细","row_start":2,"row_end":2}`},
		{Start: 4, End: 9, LocatorJson: `{"type":"sheet","sheet":"明细","row_start":50001,"row_end":50001}`},
		{Start: 5, End: 5, LocatorJson: `{"type":"sheet"}`},
		{Start: 0, End: 1, LocatorJson: `invalid`},
	}
}

func (*sourceTransportServer) Read(context.Context, *proto.ReadRequest) (*proto.ReadResponse, error) {
	return &proto.ReadResponse{MarkdownContent: "首行甲乙最后一行丙", SourceBlocks: sourceTransportBlocks()}, nil
}

func (s *sourceTransportServer) ReadStream(_ *proto.ReadRequest, stream proto.DocReader_ReadStreamServer) error {
	if s.unary {
		return status.Error(codes.Unimplemented, "legacy reader")
	}
	return stream.Send(&proto.ReadStreamResponse{Payload: &proto.ReadStreamResponse_Meta{
		Meta: &proto.ReadStreamMeta{MarkdownContent: "首行甲乙最后一行丙", SourceBlocks: sourceTransportBlocks()},
	}})
}

func requireTransportedSourceRows(t *testing.T, result *types.ReadResult) {
	t.Helper()
	require.Equal(t, "首行甲乙最后一行丙", result.MarkdownContent)
	require.Equal(t, []types.SourceBlock{
		{Start: 0, End: 4, Locator: types.SourceLocator{Type: "sheet", Sheet: "明细", RowStart: 2, RowEnd: 2}},
		{Start: 4, End: 9, Locator: types.SourceLocator{Type: "sheet", Sheet: "明细", RowStart: 50001, RowEnd: 50001}},
	}, result.SourceBlocks)
}

func TestSourceTransportGRPCPreservesRows(t *testing.T) {
	for _, unary := range []bool{false, true} {
		t.Run(map[bool]string{false: "stream", true: "unary_fallback"}[unary], func(t *testing.T) {
			listener := bufconn.Listen(1 << 20)
			t.Cleanup(func() { _ = listener.Close() })
			server := grpc.NewServer()
			proto.RegisterDocReaderServer(server, &sourceTransportServer{unary: unary})
			t.Cleanup(server.Stop)
			go func() { _ = server.Serve(listener) }()
			conn, err := grpc.NewClient("passthrough:///source-transport",
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			reader := &GRPCDocumentReader{conn: conn, client: proto.NewDocReaderClient(conn)}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := reader.Read(ctx, &types.ReadRequest{FileName: "rows.xlsx", FileType: "xlsx"})
			require.NoError(t, err)
			requireTransportedSourceRows(t, result)
		})
	}
}

func TestSourceTransportHTTPPreservesRows(t *testing.T) {
	var response httpReadResponse
	require.NoError(t, json.Unmarshal([]byte(`{
	 "markdown_content":"首行甲乙最后一行丙",
	 "source_blocks":[
	 {"start":0,"end":4,"locator_json":"{\"type\":\"sheet\",\"sheet\":\"明细\",\"row_start\":2,\"row_end\":2}"},
	 {"start":4,"end":9,"locator_json":"{\"type\":\"sheet\",\"sheet\":\"明细\",\"row_start\":50001,\"row_end\":50001}"},
	 {"start":5,"end":5,"locator_json":"{\"type\":\"sheet\"}"},
	 {"start":0,"end":1,"locator_json":"invalid"}]
	}`), &response))
	requireTransportedSourceRows(t, fromHTTPReadResponse(&response))
}
