package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

const consumerEvidenceLimit = 32

type consumerEvidenceKey struct{}

// Raw metadata is retained only inside the probe, never serialized. Diagnostics
// are observational and cannot alter the result of the existing inspector.
type consumerEvidence struct {
	ExpectedQueues                []string                  `json:"expected_queues"`
	Class                         string                    `json:"class"`
	ElapsedMS                     int64                     `json:"elapsed_ms"`
	MembershipRevision            string                    `json:"membership_revision"`
	PostFailureMembershipRevision string                    `json:"post_failure_membership_revision"`
	ExpectedIncarnation           string                    `json:"expected_incarnation"`
	ObservedIncarnation           string                    `json:"observed_incarnation,omitempty"`
	AdmittedIncarnation           string                    `json:"admitted_incarnation,omitempty"`
	IncarnationMatches            bool                      `json:"incarnation_matches"`
	ReadClass                     string                    `json:"read_class"`
	ProbeClass                    string                    `json:"probe_class"`
	RegistryCount                 int                       `json:"registry_count"`
	RegistryClass                 string                    `json:"registry_class"`
	Truncated                     bool                      `json:"truncated"`
	ConsumerServerIDs             []string                  `json:"consumer_server_ids"`
	PreviousConsumerServerIDs     []string                  `json:"previous_consumer_server_ids"`
	MissingPreviousServerIDs      []string                  `json:"missing_previous_server_ids"`
	Servers                       []*consumerServerEvidence `json:"servers"`
	client                        redis.UniversalClient
	reads                         map[string]*consumerServerEvidence
	started                       time.Time
}
type consumerServerEvidence struct {
	ServerID        string          `json:"server_id"`
	MetadataClass   string          `json:"metadata_class"`
	TTLMS           int64           `json:"post_failure_ttl_ms"`
	RegistryScore   float64         `json:"post_failure_registry_score"`
	ScorePresent    bool            `json:"score_present"`
	ReadClass       string          `json:"post_failure_read_class"`
	ExpiryEvidence  string          `json:"expiry_evidence"`
	QueueSubscribed map[string]bool `json:"queue_subscribed,omitempty"`
	IdentityMatches bool            `json:"key_identity_matches_metadata"`
	raw             string
}

func newConsumerEvidence(ctx context.Context, client redis.UniversalClient, approved bool) *consumerEvidence {
	d := &consumerEvidence{
		client:         client,
		started:        time.Now(),
		ExpectedQueues: []string{"sync"},
		reads:          map[string]*consumerServerEvidence{},
	}
	if approved {
		d.ExpectedQueues = append(d.ExpectedQueues, ApprovedSyncQueue)
	}
	if dest, ok := ctx.Value(consumerEvidenceKey{}).(**consumerEvidence); ok {
		*dest = d
	}
	return d
}

func evidenceServerID(key string) string {
	if len(key) > 256 || !strings.HasPrefix(key, "asynq:servers:{") || !strings.HasSuffix(key, "}") {
		return "invalid"
	}
	id := strings.TrimSuffix(key[strings.LastIndex(key, ":")+1:], "}")
	if parsed, err := uuid.Parse(id); err == nil && parsed.String() == id {
		return id
	}
	return "invalid"
}

func consumerReadClass(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, ErrNotReady) {
		return "expected_queue_not_subscribed"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errConsumerAbsent) ||
		errors.Is(err, errConsumerInactive) {
		return failureClass(err)
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "redis_timeout"
	}
	var conn *net.OpError
	if errors.As(err, &conn) {
		return "redis_connection_error"
	}
	return "redis_read_error"
}

func (d *consumerEvidence) read(key, raw string, err error) {
	if !strings.HasPrefix(key, "asynq:servers:{") {
		return
	}
	if len(key) > 256 {
		d.Truncated = true
		return
	}
	if len(d.reads) >= consumerEvidenceLimit {
		d.Truncated = true
		return
	}
	s := &consumerServerEvidence{ServerID: evidenceServerID(key), TTLMS: -3, MetadataClass: "metadata_ok"}
	d.reads[key] = s
	d.Servers = append(d.Servers, s)
	if errors.Is(err, redis.Nil) {
		s.MetadataClass = "metadata_get_missing"
		return
	}
	if err != nil {
		s.MetadataClass = consumerReadClass(err)
		return
	}
	if len(raw) > 2048 {
		s.MetadataClass = "metadata_decode_unavailable"
		return
	}
	s.raw = raw
}

func (d *consumerEvidence) decode(key string, s *consumerServerEvidence) {
	if s.MetadataClass != "metadata_ok" {
		return
	}
	// Use the descriptor registered by the pinned Asynq dependency, without
	// importing its internal package or maintaining a second wire schema.
	desc, e := protoregistry.GlobalFiles.FindDescriptorByName("asynq.ServerInfo")
	if e != nil {
		s.MetadataClass = "metadata_decode_unavailable"
		return
	}
	messageDescriptor, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		s.MetadataClass = "metadata_decode_unavailable"
		return
	}
	msg := dynamicpb.NewMessage(messageDescriptor)
	if e = proto.Unmarshal([]byte(s.raw), msg); e != nil {
		s.MetadataClass = "metadata_decode_failed"
		return
	}
	fields := msg.Descriptor().Fields()
	metadataID := msg.Get(fields.ByName("server_id")).String()
	host := msg.Get(fields.ByName("host")).String()
	pid := msg.Get(fields.ByName("pid")).Int()
	s.IdentityMatches = key == "asynq:servers:{"+host+":"+strconv.FormatInt(pid, 10)+":"+metadataID+"}"
	queues := msg.Get(fields.ByName("queues")).Map()
	s.QueueSubscribed = map[string]bool{}
	for _, q := range d.ExpectedQueues {
		k := protoreflect.ValueOfString(q).MapKey()
		s.QueueSubscribed[q] = queues.Has(k) && queues.Get(k).Int() > 0
		if !s.QueueSubscribed[q] {
			s.MetadataClass = "expected_queue_not_subscribed"
		}
	}
	if !s.IdentityMatches {
		s.MetadataClass = "metadata_identity_mismatch"
	}
}

func incarnation(value string) string {
	_, id, ok := strings.Cut(value, ":")
	if !ok {
		return "invalid"
	}
	if u, e := uuid.Parse(id); e == nil && u.String() == id {
		return id
	}
	return "invalid"
}

// Called only after trip/stop, on the first retained failure. At most 32
// registry rows and 64 metadata commands, under one 100ms deadline. No SDK
// Inspector, Lua, retries or registry cleanup is used for additional evidence.
func (d *consumerEvidence) snapshot(ctx context.Context, c *Controller, reason string) string {
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	d.Class = reason
	d.ExpectedIncarnation = c.incarnation
	client := d.client
	switch original := client.(type) {
	case *ControlClient:
		// Production's reserved pools already honor the caller deadline.
	case *redis.Client:
		// UniversalClient callers may disable context socket deadlines or use
		// long automatic retries. Never inherit those for extra diagnostics.
		opts := *original.Options()
		opts.ContextTimeoutEnabled = true
		opts.MaxRetries = -1
		opts.DialTimeout = 100 * time.Millisecond
		opts.ReadTimeout = 100 * time.Millisecond
		opts.WriteTimeout = 100 * time.Millisecond
		opts.PoolTimeout = 100 * time.Millisecond
		opts.PoolSize = 1
		opts.MinIdleConns = 0
		bounded := redis.NewClient(&opts)
		defer func() { _ = bounded.Close() }()
		client = bounded
	default:
		d.ReadClass = "unsupported_bounded_client"
		encoded, _ := json.Marshal(d)
		return string(encoded)
	}
	if _, e := strconv.ParseUint(c.consumerRevision, 10, 64); e == nil {
		d.MembershipRevision = c.consumerRevision
	} else {
		d.MembershipRevision = "unavailable"
	}
	rows, e := client.ZRangeWithScores(ctx, "asynq:servers", 0, consumerEvidenceLimit-1).Result()
	if e != nil {
		d.ReadClass = failureClass(e)
	} else {
		d.ReadClass = "ok"
		d.RegistryCount = len(rows)
		if len(rows) == consumerEvidenceLimit {
			d.Truncated = true
		}
	}
	d.RegistryClass = "registry_read_unavailable"
	if e == nil {
		d.RegistryClass = "registry_members_present"
		if len(rows) == 0 {
			d.RegistryClass = "registry_members_absent"
		}
	}
	seen := map[string]bool{}
	for _, row := range rows {
		key, ok := row.Member.(string)
		if !ok {
			continue
		}
		seen[evidenceServerID(key)] = true
		if _, exists := d.reads[key]; !exists && len(d.reads) < consumerEvidenceLimit &&
			evidenceServerID(key) != "invalid" {
			s := &consumerServerEvidence{
				ServerID:      evidenceServerID(key),
				MetadataClass: "not_in_original_gets",
				TTLMS:         -3,
			}
			d.reads[key] = s
			d.Servers = append(d.Servers, s)
		}
	}
	if e == nil && !d.Truncated {
		for _, id := range d.PreviousConsumerServerIDs {
			if !seen[id] {
				d.MissingPreviousServerIDs = append(d.MissingPreviousServerIDs, id)
			}
		}
	}
	pipe := client.Pipeline()
	type pair struct {
		s     *consumerServerEvidence
		ttl   *redis.DurationCmd
		score *redis.FloatCmd
	}
	var requests []pair
	for key, s := range d.reads {
		d.decode(key, s)
		if evidenceServerID(key) != "invalid" {
			requests = append(requests, pair{s, pipe.PTTL(ctx, key), pipe.ZScore(ctx, "asynq:servers", key)})
		}
	}
	worker := pipe.HGet(ctx, c.prefix+"workers", c.Config.WorkerID)
	admitted := pipe.HGet(ctx, c.prefix+"admitted", c.Config.WorkerID)
	revision := pipe.Get(ctx, c.prefix+"membership_revision")
	_, _ = pipe.Exec(ctx)
	for _, r := range requests {
		r.s.ReadClass = "ok"
		if err := r.ttl.Err(); err != nil {
			r.s.ReadClass = failureClass(err)
		} else {
			ttl := r.ttl.Val()
			if ttl < 0 {
				r.s.TTLMS = int64(ttl)
			} else {
				r.s.TTLMS = ttl.Milliseconds()
			}
		}
		if err := r.score.Err(); err == nil && !math.IsInf(r.score.Val(), 0) && !math.IsNaN(r.score.Val()) {
			r.s.RegistryScore = r.score.Val()
			r.s.ScorePresent = true
			if r.s.MetadataClass == "metadata_get_missing" && r.score.Val() <= float64(time.Now().Unix()) {
				r.s.MetadataClass = "metadata_missing_expired_registry_score"
			}
		} else if !errors.Is(err, redis.Nil) {
			r.s.ReadClass = failureClass(err)
		}
		r.s.ExpiryEvidence = "unknown"
		if r.s.ReadClass == "ok" && r.s.TTLMS == -2 {
			r.s.ExpiryEvidence = "key_missing_expiry_or_deletion_unknown"
		}
		if r.s.ReadClass == "ok" && r.s.TTLMS >= 0 && r.s.TTLMS <= 10 {
			r.s.ExpiryEvidence = "ttl_at_expiry_boundary"
		}
		if r.s.ScorePresent && r.s.RegistryScore <= float64(time.Now().Unix()) {
			r.s.ExpiryEvidence = "registry_score_elapsed"
		}
	}
	if worker.Err() == nil {
		d.ObservedIncarnation = incarnation(worker.Val())
	}
	if admitted.Err() == nil {
		d.AdmittedIncarnation = incarnation(admitted.Val())
	}
	d.IncarnationMatches = d.ExpectedIncarnation == d.ObservedIncarnation &&
		d.ExpectedIncarnation == d.AdmittedIncarnation
	if revision.Err() == nil {
		if _, err := strconv.ParseUint(revision.Val(), 10, 64); err == nil {
			d.PostFailureMembershipRevision = revision.Val()
		}
	}
	encoded, _ := json.Marshal(d)
	return string(encoded)
}
